![](assets/repository-hero.png)

## Prolog

AMPS arose from our necessity to dynamically scale asynchronous worker queue jobs
and enabling workers to process several jobs in the same container in parallel.
We chose his architecture to have instantaneous worker startup times for a specific maximum amount of parallel jobs without having to wait for a cold start of a container.

AMPS represents a sidecar container that subscribes to a queue such as RabbitMQ and consumes
messages from it, until the max concurrency limit is met. Upon receiving a message, AMPS
will send a HTTP request to the workload container, which will acknowledge that it has received the job by just simply sending back a valid HTTP response code.
The workload can then asynchronously work on that message within the workload timeout.
Upon successful completion of the workload, it will send a `acknowledge` HTTP request back to the AMPS
sidecar container to finalize the Job completion. A workload can also send back a `reject` request, letting the AMPS container know, that the job could not successfully be processes.
Furthermore, the AMPS container allows publishing of new jobs through its `publish` endpoint, enabling you to chain several AMPS containers together.

Since AMPS holds an internal state of which job is currently worked on, it knows how to handle and how to timeout concurrently running jobs.

**Note:** AMPS is supposed to do one job well, and one job only. Other popular FaaS-like projects have often seen a huge amount of feature creep over the years, making them outdated, fragile and inefficient.

### Why use HTTP for your workload?

We chose HTTP as a simple method to convey a job to the workload without the workload having to use any special libraries,
other than a HTTP server. HTTP also gives us the native ability to run processes/workloads in parallel, without having to do
any custom process handling on the development side, which of course you can still do if needed.
This pattern is well established in the FaaS community, and thus is the reasonable choice for this kind of application.

### Job message structure

AMPS is built, such that it always expects a CloudEvent formatted message body. The spec-version is currently set to `1.0.0`
and all queue messages should be formatted accordingly.

AMPS also conveys this CloudEvent to the Workload by the **structured content mode**.
This means, that it will pack the whole CloudEvent into a JSON and `POST` it to the workload with the `Content-Type: application/json` header.

### Kubernetes

This container is primarily designed to work in a Kubernetes environment. We can't ensure, that this container will work with other
systems such as OpenStack, ECS, etc., although it should be easily possible.

### Scalability

AMPS does not support scaling up and down by itself _at the moment_. To scale AMPS-Workload Pods you will need to choose your own
scaling solution. We have had great success with using [KEDA](https://keda.sh/) or the HorizontalPodAutoscaler in Kubernetes.

Scaling always depends on your specific needs, thus implementing a native scaling solution for AMPS would never cover all use cases. We encourage you to build your own autoscaling system and are open for any features you wish to see implemented. Since we do take care, that the architecture of the project is a modular as possible, it may be possible to build a dynamic autoscaling solution architecture thad does not interfere with the core logic of the container. Stay tuned for RFCs.

## Getting started

As an example we'll show you how you can use this container as a Sidecar container in your deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: your-deployment-name
  labels:
    app: your-deployment-name
spec:
  # Scale your worker initially
  replicas: 10
  selector:
    matchLabels:
      app: your-deployment-name
  template:
    metadata:
      labels:
        app: your-deployment-name
    spec:
      # Create the AMPS sidecar container
      containers:
        - name: amps
          # Fetch the container image. Make sure to pin the version in production
          image: registry.gitlab.com/hotellistat/amps
          # Make sure the container is always in a healthy state
          livenessProbe:
            httpGet:
              path: /healthz
              port: 4000
            initialDelaySeconds: 3
            periodSeconds: 3
          ports:
            # This will be the port on which the AMPS HTTP server will run on. The port is hardcoded to 4000 for now
            - containerPort: 4000

          # Environment configuration of the container.
          # Have a look at /cmd/config/config.go for further configuration details
          env:
            - name: BROKER_HOST
              value: 'amqp://username:password@rabbitmq:5672'
            - name: BROKER_SUBJECT
              value: com.yourorg.some-message-queue
            - name: JOB_TIMEOUT
              value: '1h'
            - name: DEBUG
              value: 'true'
            - name: MAX_CONCURRENCY
              value: '20'
            - name: WORKLOAD_ADDRESS
              value: 'http://localhost:80'

        # This container represents your workload.
        # The workload has to have a HTTP server running on the defined port
        # as configured in the "WORKLOAD_ADDRESS" environment variable
        # (WORKLOAD_ADDRESS env config in the AMPS container)
        # for the AMPS container to be able to send new messages to the workload.
        - name: workload
          image: alpine
          ports:
            - containerPort: 80
```

This `yaml` schema can of course also be converted into any other k8s object that wraps a Pod/Deployment template.

## Sequence diagram

Sometimes a simple diagram explains more than a thousand words:

![](assets/sequence.svg)

## Architectural overview

![](assets/architecture.png)

## Development

Developing AMPS is a simple as it gets. Follow these steps:

1. Copy the `.env.template` file and rename it to `.env`
2. Set the `.env` environment variables to your specification (have a look into [`cmd/amps/config/config.go`](/cmd/amps/config/config.go) for further configuration settings)
3. Launch a local RabbitMQ server (`docker-compose up rabbitmq`)
4. Launch the test webserver in the hack folder (`make server`)
5. Configure the environment variables in the `.env` for your system
6. Run `make dev`

The application should just start up and listen for any new messages in the message broker.

## TODO

- [x] Support RabbitMQ
- [ ] Full unit tests
- [ ] Add full testing pipeline
- [ ] End to End tests
- [x] Prometheus metrics endpoint
- [ ] Synchronous mode (FaaS)
- [x] Configuration should support multiple message brokers
- [ ] Parallel subscriptions
