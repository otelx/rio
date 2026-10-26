# Provenance and upstream relationships

RIO is an independent otelx project. Its first source snapshot was extracted
from the controller and payload components in
[OpenTelemetry Far and Wide](https://github.com/MrAlias/OpenTelemetry-Far-and-Wide/tree/2a3b7809cbca964651ae6a916279833d11c8cdde/components),
revision `2a3b7809cbca964651ae6a916279833d11c8cdde`, and adapted for standalone use.
Its Git history begins with that extraction. The original presentation's code,
media, recordings, synthetic voices, and historical evidence are not distributed
as part of RIO.

## Grafana prototype

The earlier design explored in the presentation was Grafana's
`hackathon-16-composite-sdk-injection` branch of the OpenTelemetry Operator,
reviewed at revision `552f4c550fb58be21721191d5704ba501933b1ef`.
Its [prototype documentation](https://github.com/grafana/opentelemetry-operator/blob/552f4c550fb58be21721191d5704ba501933b1ef/hackathon/README.md)
describes central selection and runtime-aware SDK injection. The presentation's
source review records Jack Berg's implementation work and Nikola Grcevski's
participation in its design.

RIO's controller is the later, independent implementation from the presentation
repository. It uses its own API and rollout/recovery logic. It is not a fork of
that historical Operator branch, and the branch's feature set and limitations
must not be attributed to RIO without verification. The separation between
admission and payload delivery is also illustrated by
[Grafana's Kubernetes injection controller](https://github.com/grafana/k8s-injection-controller).

## OpenTelemetry dependencies

The payload packages the upstream
[OpenTelemetry Injector](https://github.com/open-telemetry/opentelemetry-injector)
and language agents. RIO does not reimplement those agents or maintain a fork of
the Injector. Exact input versions, source revisions, checksums, and dependency
locks live under [payload/](../payload/).

The Python requirements originate from the upstream OpenTelemetry Operator's
Python packaging. The Ruby dependency graph and payload layout draw on Grafana
Beyla's webhook image packaging. Attribution is retained in
[NOTICE](../NOTICE) and [payload/NOTICE](../payload/NOTICE).

The upstream OpenTelemetry Operator supports language-specific namespace and
workload opt-in. RIO's central selection and runtime detection should not be
presented as evidence that upstream requires a separate annotation per service.
OpenTelemetry eBPF Instrumentation (OBI) remains a separate component. RIO does
not implement the presentation's historical OBI handoff experiments.
