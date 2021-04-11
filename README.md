# Batchable

## Prolog

Batchable arose from our necessity to dynamically scale asynchronous worker queue jobs
and enabling those workers to work on several jobs in the same container in parallel.

Batchable represents a sidecar container that subscribes to a queue such as NATS and consumes
messages from that queue, until the max concurrency limit is met. Upon receiving a message, Batchable
will send a HTTP request to the workload container, which will acknowledge that it has received the job.
The workload can then asynchronously work on that message within the workload timeout.
Upon successful completion of the workload, it will send a `checkout` HTTP request back to the Batchable
sidecar container to finalize the Job completion.

Since Batchable holds an internal state of which job is currently worked on, it knows how to handle and how to timeout
concurrently running jobs.

Batchable is supposed to do one job well, and one job only. Other solutions such as OpenFaaS tried to support more and more features
as they progressed, resulting in loads of complexity and inefficiencies.
Batchable is supposed to be as configurable as possible to adapt to your needs.

### Why use HTTP for your workload?

We chose HTTP as a simple method to convey a job to the workload without the workload having to use any special libraries,
other than a HTTP server. HTTP also gives us the native ability to run processes/workload in parallel, without having to do
any process handling on the development side. This pattern is well established in the FaaS community, and thus is the reasonable
choice for this kind of application.

### Job message structure

Batchable is built, such that it always expects a CloudEvent formatted message body. The specversion is currently set to `1.0.0`
and all queue messages should be formatted accordingly.

Batchable also conveys this CloudEvent to the Workload by the **structured content mode**.
This means, that it will pack the whole CloudEvent into a JSON and pass it to the workload with the `Content-Type: application/json` header.

### Kubernetes

This container is primarily designed to work in a Kubernetes environment. We can't ensure, that this container will work with other
systems such as OpenStack, ECS, etc.

### Scalability

Batchable does not support scaling up and down _at the moment_. To scale Batchable-Workload Pods you will need to choose your own
scaling solution. We have had great success with using [KEDA](https://keda.sh/).

Scaling always depends on your specific needs, thus building our own scaling solution for Batchable wouldn't cover all use cases,
thus restricting the application usability.

## Getting started

As an example we'll show you how you can use this container as a Sidecar container in your deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: yourworkername
  labels:
    app: yourworkername
spec:
  # Scale your worker initially
  replicas: 10
  selector:
    matchLabels:
      app: yourworkername
  template:
    metadata:
      labels:
        app: yourworkername
    spec:
      # Create the Batchable sidecar container
      containers:
        - name: batchable
          # Fetch the container image. Make sure to pin the version in production
          image: registry.gitlab.com/hotellistat/batchable
          # Make sure the container is always in a healthy state
          livenessProbe:
            httpGet:
              path: /healthz
              port: 4000
            initialDelaySeconds: 3
            periodSeconds: 3
          ports:
            # This will be the port on which the Batchable HTTP server will run on. The port is hardcoded to 4000 for now
            - containerPort: 4000

          # Environment configuration of the container.
          # Have a look at /cmd/config/config.go for further configuration details
          env:
            - name: BROKER_HOST
              value: "nats.messaging:4222"
            - name: BROKER_CLUSTER
              value: nats
            - name: BROKER_SUBJECT
              value: com.hotellistat.yourworkername
            - name: BROKER_DURABLE_GROUP
              value: com.hotellistat.yourworkername
            - name: BROKER_QUEUE_GROUP
              value: com.hotellistat.yourworkername
            - name: JOB_TIMEOUT
              value: "6h"
            - name: DEBUG
              value: "true"
            - name: MAX_CONCURRENCY
              value: "20"
            - name: WORKLOAD_ADDRESS
              value: "http://localhost:80"

        # This container represents your workload.
        # The workload has to have a HTTP server running on port 80
        # (WORKLOAD_ADDRESS env config in the batchable container)
        # for the batchable container to be able to send new messages to the workload.
        - name: workload
          image: alpine
          ports:
            - containerPort: 80
```

This `yaml` schema can of course also be converted into any other k8s object that wraps a Pod template.

## Development

Developing Batchable is a simple as it gets. Follow these steps:

1. Copy the `.env.template` file and rename it to `.env`
2. Launch a local NATS server
3. Configure the environment variables in the `.env` for your system
4. Run `make dev`

The application should just start up and listen for any new messages in the message broker.

## TODO

- [ ] Support RabbitMQ
- [ ] Full unit tests
- [ ] Add full testing pipeline
- [ ] End to End tests
- [ ] Prometheus metrics endpoint
- [ ] Synchronous mode (FaaS)
- [ ] Configuration should support multiple message brokers
- [ ] Parallel subscriptions
