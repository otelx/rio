# syntax=docker/dockerfile:1
ARG BASE_IMAGE=python:3.14.6-trixie@sha256:55dd7c1ea6c2b2c45a26d797187706eb9e5e0046f8e54fd759e38647b505c36b
FROM ${BASE_IMAGE}
WORKDIR /app
COPY requirements.lock ./
RUN pip install --no-cache-dir --require-hashes --only-binary=:all: -r requirements.lock
COPY app.py ./
EXPOSE 8080
CMD ["python", "/app/app.py"]
