#!/usr/bin/env python3
"""Admission -> shipped Injector/agents -> real Collector integration checks.

Every invocation owns a disposable cluster and retains all evidence on failure.
No fixture includes an OpenTelemetry SDK or creates telemetry explicitly.
"""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import uuid

sys.path.insert(0, str(Path(__file__).parent))
from telemetry.assertions import items, read_records, server_span

ROOT = Path(__file__).resolve().parent.parent
COLLECTOR = 'otel/opentelemetry-collector-contrib:0.163.2@sha256:7f22d2a6748110ffb6a59bc0df448bccbf501cdb667d610ede4c61d3863d5849'


class Harness:
    def __init__(self, args):
        self.args = args
        self.run_id = args.resume or 'rio-issue-17-' + uuid.uuid4().hex[:10]
        assert re.fullmatch(r'rio-issue-17-[0-9a-f]{10}', self.run_id), 'Invalid owned run ID'
        self.cluster = self.run_id
        base = ROOT / '.artifacts/telemetry' / self.run_id
        if args.resume:
            assert (base / 'kubeconfig').is_file(), 'Resume requires this worktree\'s saved kubeconfig'
        self.out = base / ('retry-' + uuid.uuid4().hex[:8]) if args.resume else base
        self.out.mkdir(parents=True)
        self.env = dict(os.environ, KUBECONFIG=str(base / 'kubeconfig'))
        self.operator = os.environ.get('OPERATOR_IMAGE', 'ghcr.io/otelx/rio/operator:issue-17')
        self.payload = os.environ.get('PAYLOAD_IMAGE', 'ghcr.io/otelx/rio/instrumentation:issue-17')
        self.namespace = 'rio-fixtures'
        self.created = False
        self.counter = 0
        self.current = self.out
        self.config = {}
        self.expected_config = None

    def command(self, *argv, timeout=180, check=True, input=None):
        try:
            result = subprocess.run(argv, env=self.env, text=True, input=input,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            output = exc.stdout or ''
            if isinstance(output, bytes):
                output = output.decode(errors='replace')
            with (self.out / 'commands.log').open('a') as log:
                log.write(json.dumps(list(argv)) + '\nTIMEOUT\n' + output + '\n')
            raise
        with (self.out / 'commands.log').open('a') as log:
            log.write(json.dumps(list(argv)) + '\n' + result.stdout + '\n')
        if check and result.returncode:
            raise RuntimeError(f'Command failed ({result.returncode}): {argv}\n{result.stdout}')
        return result.stdout

    def kube(self, *argv, **kwargs):
        return self.command('kubectl', '--request-timeout=30s', *argv, **kwargs)

    def apply(self, obj):
        self.counter += 1
        (self.current / f'manifest-{self.counter}.json').write_text(json.dumps(obj, indent=2))
        self.kube('apply', '-f', '-', input=json.dumps(obj))

    def setup(self):
        # A nonroot workload must be able to read the shipped configuration,
        # regardless of the checkout umask used to build the payload.
        self.command('docker', 'run', '--rm', '--user', '65532:65532', self.payload,
                     '/bin/sh', '-ec', '/verify-layout.sh; cat /dist/injector/otelinject.conf >/dev/null')
        existing = self.command('kind', 'get', 'clusters').splitlines()
        if self.args.resume:
            assert self.cluster in existing, 'Saved cluster no longer exists'
            assert self.kube('config', 'current-context').strip() == 'kind-' + self.cluster
        else:
            assert self.cluster not in existing
        config = 'kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nfeatureGates:\n  ImageVolume: ' + str(self.args.delivery == 'imageVolume').lower() + '\nnodes:\n  - role: control-plane\n'
        path = self.out / 'kind.yaml'
        path.write_text(config)
        self.created = True
        if not self.args.resume:
            self.command('kind', 'create', 'cluster', '--retain', '--name', self.cluster, '--image', 'kindest/node:v1.35.0',
                         '--config', str(path), '--kubeconfig', self.env['KUBECONFIG'], '--wait', '180s', timeout=300)
        self.command('kind', 'load', 'docker-image', '--name', self.cluster, self.operator, self.payload, timeout=300)
        if self.args.resume:
            self.kube('delete', 'namespace', self.namespace, 'rio-telemetry', '--ignore-not-found', '--wait=true', '--timeout=180s', timeout=210)
            self.kube('delete', 'instrumentations.rio.otelx.github.io', 'cluster', '--ignore-not-found', '--wait=true', '--timeout=180s', check=False)
        self.command(str(ROOT / 'scripts/install.sh'), self.operator, timeout=300)
        nodes = json.loads(self.kube('get', 'nodes', '-o', 'json'))
        (self.out / 'nodes.json').write_text(json.dumps(nodes, indent=2))
        assert {n['status']['nodeInfo']['architecture'] for n in nodes['items']} == {self.args.arch}
        self.kube('create', 'namespace', self.namespace)
        self.kube('create', 'namespace', 'rio-telemetry')
        self.collector()
        self.config = {lang: json.loads((ROOT / 'scripts/telemetry/fixtures' / lang / 'fixture.json').read_text())
                       for lang in self.args.languages}
        for lang, spec in self.config.items():
            for libc in self.args.libcs:
                image = self.image(lang, libc)
                self.command('docker', 'build', '--build-arg', 'BASE_IMAGE=' + spec['images'][libc],
                             '-t', image, str(ROOT / 'scripts/telemetry/fixtures' / lang), timeout=600)
                self.command('kind', 'load', 'docker-image', '--name', self.cluster, image, timeout=300)
                (self.out / f'image-{lang}-{libc}.json').write_text(self.command('docker', 'image', 'inspect', image))
                runtime = {'node': 'node --version', 'java': 'java -version', 'dotnet': 'dotnet --info',
                           'python': 'python --version', 'ruby': 'ruby --version'}[lang]
                (self.out / f'runtime-{lang}-{libc}.log').write_text(self.command('docker', 'run', '--rm',
                    '--entrypoint', '/bin/sh', image, '-c', runtime + '; cat /etc/os-release; ldd --version 2>&1 || test "$?" -eq 1'))
        (self.out / 'versions.json').write_text(json.dumps({'fixtures': self.config, 'collector': COLLECTOR,
            'agents': json.loads((ROOT / 'payload/versions.json').read_text()), 'delivery': self.args.delivery}, indent=2))
        for kind, image in [('operator', self.operator), ('instrumentation', self.payload)]:
            (self.out / f'{kind}-image.json').write_text(self.command('docker', 'image', 'inspect', image))

    def image(self, lang, libc):
        return f'{self.run_id}-{lang}-{libc}:test'

    def collector(self):
        config = 'receivers:\n  otlp:\n    protocols:\n      http:\n        endpoint: 0.0.0.0:4318\nexporters:\n'
        for signal in ('traces', 'metrics', 'logs'):
            config += f'  file/{signal}:\n    path: /data/{signal}.json\n'
        config += 'service:\n  pipelines:\n'
        for signal in ('traces', 'metrics', 'logs'):
            config += f'    {signal}:\n      receivers: [otlp]\n      exporters: [file/{signal}]\n'
        self.apply({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': 'collector', 'namespace': 'rio-telemetry'}, 'data': {'config.yaml': config}})
        pod = {'containers': [
            {'name': 'collector', 'image': COLLECTOR, 'args': ['--config=/conf/config.yaml'],
             'securityContext': {'runAsUser': 0}, 'ports': [{'containerPort': 4318}],
             'readinessProbe': {'tcpSocket': {'port': 4318}},
             'volumeMounts': [{'name': 'config', 'mountPath': '/conf'}, {'name': 'data', 'mountPath': '/data'}]},
            {'name': 'reader', 'image': self.payload, 'imagePullPolicy': 'IfNotPresent',
             'command': ['/bin/sh', '-c', 'exec sleep infinity'], 'volumeMounts': [{'name': 'data', 'mountPath': '/data'}]}],
            'volumes': [{'name': 'config', 'configMap': {'name': 'collector'}}, {'name': 'data', 'emptyDir': {}}]}
        self.apply({'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': 'collector', 'namespace': 'rio-telemetry'},
            'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': 'collector'}}, 'template': {'metadata': {'labels': {'app': 'collector'}}, 'spec': pod}}})
        self.apply({'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'collector', 'namespace': 'rio-telemetry'},
            'spec': {'selector': {'app': 'collector'}, 'ports': [{'port': 4318}]}})
        self.kube('rollout', 'status', 'deployment/collector', '-n', 'rio-telemetry', '--timeout=180s')

    def policy(self, signals, rule_signals=None, env=None):
        agent_env = {'OTEL_BSP_SCHEDULE_DELAY': '500', 'OTEL_METRIC_EXPORT_INTERVAL': '1000',
                     'OTEL_BLRP_SCHEDULE_DELAY': '500', 'OTEL_PROPAGATORS': 'tracecontext,baggage',
                     'OTEL_TRACES_SAMPLER': 'always_on', 'OTEL_SEMCONV_STABILITY_OPT_IN': 'http'}
        agent_env.update(env or {})
        rule = {'name': 'fixtures', 'action': 'Inject', 'selector': {'namespaces': [self.namespace]}}
        if rule_signals is not None:
            rule['signals'] = rule_signals
        self.expected_config = {'signals': dict(signals, **(rule_signals or {})), 'env': agent_env}
        self.apply({'apiVersion': 'rio.otelx.github.io/v1alpha1', 'kind': 'Instrumentation', 'metadata': {'name': 'cluster'},
            'spec': {'image': self.payload, 'endpoint': 'http://collector.rio-telemetry.svc:4318',
                     'signals': signals, 'timeoutSeconds': 300,
                     'env': [{'name': k, 'value': v} for k, v in agent_env.items()], 'rules': [rule]}})

    def deploy(self, lang, libc, name, injected):
        env = {'OTEL_SERVICE_NAME': name}
        env.update(self.config[lang].get('env', {}))
        self.apply({'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': name, 'namespace': self.namespace},
            'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': name}},
            'template': {'metadata': {'labels': {'app': name}}, 'spec': {'terminationGracePeriodSeconds': 5,
                'nodeSelector': {'kubernetes.io/arch': self.args.arch}, 'containers': [{'name': 'app', 'image': self.image(lang, libc),
                    'imagePullPolicy': 'IfNotPresent', 'ports': [{'containerPort': 8080}],
                    'env': [{'name': k, 'value': v} for k, v in env.items()],
                    'readinessProbe': {'tcpSocket': {'port': 8080}, 'periodSeconds': 2}}]}}}})
        self.apply({'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': name, 'namespace': self.namespace},
            'spec': {'selector': {'app': name}, 'ports': [{'port': 8080}]}})
        self.kube('rollout', 'status', 'deployment/' + name, '-n', self.namespace, '--timeout=360s', timeout=390)
        def admitted():
            pods = json.loads(self.kube('get', 'pods', '-n', self.namespace, '-l', 'app=' + name, '-o', 'json'))
            (self.current / (name + '-pods.json')).write_text(json.dumps(pods, indent=2))
            active = [p for p in pods['items'] if not p['metadata'].get('deletionTimestamp')]
            assert len(active) == 1, active
            annotations = active[0]['metadata'].get('annotations', {})
            assert all(c.get('restartCount', 0) == 0 for c in active[0].get('status', {}).get('containerStatuses', [])), active[0]['status']
            if injected:
                assert annotations.get('rio.otelx.github.io/delivery') == self.args.delivery, annotations
                assert annotations['rio.otelx.github.io/applied-revision'] == annotations['rio.otelx.github.io/revision']
                config = json.loads(annotations['rio.otelx.github.io/configuration'])
                assert config['signals'] == self.expected_config['signals'], config
                assert config['image'] == self.payload, config
                assert config['endpoint'] == 'http://collector.rio-telemetry.svc:4318', config
                assert {e['name']: e['value'] for e in config['env']} == self.expected_config['env'], config
                volume = next(v for v in active[0]['spec']['volumes'] if v['name'] == 'rio-payload')
                if self.args.delivery == 'imageVolume':
                    assert volume['image']['reference'] == self.payload, volume
                else:
                    assert 'emptyDir' in volume and 'image' not in volume, volume
                    assert any(c['image'] == self.payload and any(m['name'] == 'rio-payload' for m in c.get('volumeMounts', []))
                               for c in active[0]['spec']['initContainers']), active[0]['spec']
            else:
                assert not any(e['name'] == 'LD_PRELOAD' for c in active[0]['spec']['containers'] for e in c.get('env', []))
            assert any(c['type'] == 'Ready' and c['status'] == 'True' for c in active[0].get('status', {}).get('conditions', []))
        # Capabilities may still be probing when an initial uninstrumented Pod
        # starts; wait for the controller's admitted replacement to be Ready.
        self.poll(admitted, timeout=360)
        return name

    def request(self, name, marker, headers=None, next_url=None):
        log_path = self.current / (name + '-' + marker + '-forward.log')
        with log_path.open('w') as log:
            forward = subprocess.Popen(['kubectl', 'port-forward', '-n', self.namespace, 'service/' + name, ':8080'],
                                       env=self.env, stdout=log, stderr=subprocess.STDOUT, text=True)
            try:
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline:
                    match = re.search(r'127\.0\.0\.1:(\d+)', log_path.read_text())
                    if match:
                        break
                    if forward.poll() is not None:
                        raise RuntimeError(log_path.read_text())
                    time.sleep(.2)
                else:
                    raise TimeoutError('port-forward did not start')
                url = f'http://127.0.0.1:{match[1]}/work/{marker}'
                if next_url:
                    url += '?' + urllib.parse.urlencode({'next': next_url})
                with urllib.request.urlopen(urllib.request.Request(url, headers=headers or {}), timeout=30) as response:
                    body = json.load(response)
                assert body['marker'] == marker, body
                (self.current / (marker + '-response.json')).write_text(json.dumps(body, indent=2))
                return body
            finally:
                forward.terminate()
                try:
                    forward.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    forward.kill()
                    forward.wait()

    def records(self, signal):
        text = self.kube('exec', '-n', 'rio-telemetry', 'deployment/collector', '-c', 'reader', '--',
                         'sh', '-c', f'cat /data/{signal}.json 2>/dev/null || true')
        (self.current / (signal + '.jsonl')).write_text(text)
        return read_records(text)

    def poll(self, assertion, timeout=60):
        deadline = time.monotonic() + timeout
        error = None
        while time.monotonic() < deadline:
            try:
                return assertion()
            except (AssertionError, KeyError, json.JSONDecodeError) as exc:
                error = exc
                time.sleep(2)
        raise AssertionError(f'Telemetry assertion timed out: {error}') from error

    def trace_cases(self):
        for lang in self.args.languages:
            for libc in self.args.libcs:
                self.current = self.out / f'{lang}-{libc}'
                self.current.mkdir()
                self.kube('delete', 'instrumentations.rio.otelx.github.io', 'cluster', '--ignore-not-found', '--wait=true', '--timeout=180s')
                base = f'{lang}-{libc}'
                name = self.deploy(lang, libc, base + '-bare', False)
                marker = uuid.uuid4().hex
                self.request(name, marker)
                self.assert_silent(name, ('traces', 'metrics', 'logs'))
                try:
                    server_span(self.records('traces'), name, marker)
                except AssertionError:
                    pass
                else:
                    raise AssertionError('Trace assertion accepted deliberately uninstrumented app')
                self.save_apps()
                # Existing baseline must stay uninstrumented for this negative control.
                self.kube('delete', 'deployment', name, '-n', self.namespace, '--wait=true')
                self.policy({'traces': True, 'metrics': False, 'logs': False})
                name = self.deploy(lang, libc, base + '-traces', True)
                marker = uuid.uuid4().hex
                self.request(name, marker)
                self.poll(lambda: server_span(self.records('traces'), name, marker))
                self.assert_silent(name, ('metrics', 'logs'), duration=3)
                self.policy({'traces': False, 'metrics': False, 'logs': False})
                name = self.deploy(lang, libc, base + '-disabled', True)
                marker = uuid.uuid4().hex
                self.request(name, marker)
                self.assert_silent(name, ('traces', 'metrics', 'logs'))
                try:
                    server_span(self.records('traces'), name, marker)
                except AssertionError:
                    pass
                else:
                    raise AssertionError('Trace assertion accepted deliberately disabled export')
                self.save_apps()
                print(f'PASS trace export and negative controls: {lang}/{libc}/{self.args.arch}/{self.args.delivery}', flush=True)
                self.kube('delete', 'deployment', '--all', '-n', self.namespace, '--wait=true')

    def save_apps(self):
        pods = json.loads(self.kube('get', 'pods', '-n', self.namespace, '-o', 'json'))
        for pod in pods['items']:
            name = pod['metadata']['name']
            logs = self.kube('logs', '-n', self.namespace, name, '-c', 'app', check=False)
            (self.current / (name + '.log')).write_text(logs)
            assert all(c.get('restartCount', 0) == 0 for c in pod.get('status', {}).get('containerStatuses', [])), pod['status']
            assert not re.search(r'(?i)(Error loading shared library|cannot open shared object|Gem::LoadError|'
                                 r'Failed to load.*[Oo]pen[Tt]elemetry|Error in sitecustomize|'
                                 r'Attempted duplicate registration|has already been initialized|'
                                 r'ConfigurationException|OpenTelemetry.*[Cc]onfiguration.*[Ee]rror)', logs), logs

    def assert_silent(self, name, signals, duration=8):
        deadline = time.monotonic() + duration
        while time.monotonic() < deadline:
            for signal in signals:
                assert not list(items(self.records(signal), signal, name)), f'{name}: unexpected {signal}'
            time.sleep(1)

    def diagnostics(self):
        for ns in (self.namespace, 'rio-system', 'rio-telemetry'):
            (self.out / (ns + '-pods.json')).write_text(self.kube('get', 'pods', '-n', ns, '-o', 'json', check=False))
            (self.out / (ns + '-describe.log')).write_text(self.kube('describe', 'pods', '-n', ns, check=False))
            (self.out / (ns + '-events.log')).write_text(self.kube('get', 'events', '-n', ns, '--sort-by=.lastTimestamp', check=False))
            raw = self.kube('get', 'pods', '-n', ns, '-o', 'json', check=False)
            try:
                pods = json.loads(raw)
            except json.JSONDecodeError:
                continue
            for pod in pods.get('items', []):
                name = pod['metadata']['name']
                for c in pod['spec'].get('containers', []) + pod['spec'].get('initContainers', []):
                    for previous in (False, True):
                        suffix = '-previous' if previous else ''
                        logs = self.kube('logs', '-n', ns, name, '-c', c['name'], *(['--previous'] if previous else []), check=False)
                        (self.out / f'{ns}-{name}-{c["name"]}{suffix}.log').write_text(logs)
        (self.out / 'policy.json').write_text(self.kube('get', 'instrumentations.rio.otelx.github.io', '-o', 'json', check=False))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--languages', nargs='+', choices=['node', 'java', 'dotnet', 'python', 'ruby'],
                        default=['node', 'java', 'dotnet', 'python', 'ruby'])
    parser.add_argument('--libcs', nargs='+', choices=['glibc', 'musl'], default=['glibc', 'musl'])
    parser.add_argument('--arch', choices=['amd64', 'arm64'], required=True)
    parser.add_argument('--delivery', choices=['imageVolume', 'initContainer'], required=True)
    parser.add_argument('--keep-cluster', action='store_true')
    parser.add_argument('--resume', metavar='OWNED-RUN-ID', help='Retry a retained cluster with fresh namespaces/Collector and a new evidence directory')
    parser.add_argument('--allow-known-blockers', action='store_true',
                        help='Run implemented coverage while reporting unresolved upstream gaps (CI does not establish issue completion)')
    parser.add_argument('--scenarios', nargs='+', choices=['traces', 'propagation', 'metrics', 'logs'],
                        default=['traces', 'propagation', 'metrics', 'logs'])
    args = parser.parse_args()
    harness = Harness(args)
    success = False
    print(f'Evidence: {harness.out}', flush=True)
    try:
        harness.setup()
        if 'traces' in args.scenarios:
            harness.trace_cases()
        if 'propagation' in args.scenarios:
            from telemetry.propagation import run
            run(harness)
        if 'metrics' in args.scenarios:
            from telemetry.metrics import run
            blockers = run(harness)
            if blockers:
                print('INCOMPLETE automatic metrics coverage: ' + json.dumps(blockers), flush=True)
                if not args.allow_known_blockers:
                    raise RuntimeError('Automatic metrics coverage incomplete; see metrics-coverage.json and issue #22')
        if 'logs' in args.scenarios:
            from telemetry.logs import run
            run(harness)
        success = True
    finally:
        if harness.created:
            try:
                harness.diagnostics()
            except Exception as exc:
                print(f'Diagnostic collection failed: {exc}', file=sys.stderr)
            if success and not args.keep_cluster:
                harness.command('kind', 'delete', 'cluster', '--name', harness.cluster)
            else:
                print(f'Retained cluster {harness.cluster}; KUBECONFIG={harness.env["KUBECONFIG"]}', flush=True)


if __name__ == '__main__':
    main()
