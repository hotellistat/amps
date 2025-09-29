# Kubernetes Health Checks for AMPS

This document provides comprehensive guidance for configuring Kubernetes health checks (readiness and liveness probes) for AMPS to ensure reliable deployments and automatic recovery from failures.

## Health Endpoints

AMPS provides several health check endpoints designed for different monitoring purposes:

### `/healthz` - Liveness Probe
**Purpose**: Kubernetes liveness probe to determine if the container should be restarted
**When to use**: Configure as liveness probe in Kubernetes deployments
**Response**:
- `200 OK` - Application is alive and functioning
- `503 Service Unavailable` - Application is unhealthy and should be restarted

### `/readyz` - Readiness Probe
**Purpose**: Kubernetes readiness probe to determine if the container can accept traffic
**When to use**: Configure as readiness probe in Kubernetes deployments
**Response**:
- `200 OK` - Application is ready to serve traffic
- `503 Service Unavailable` - Application is not ready (initializing or broker unhealthy)

### `/health` - Legacy Compatibility
**Purpose**: Backward compatibility with existing monitoring systems
**Behavior**: Same as `/healthz`

### `/health/detailed` - Detailed Diagnostics
**Purpose**: Comprehensive health and diagnostic information for debugging
**When to use**: Manual troubleshooting, advanced monitoring systems
**Response**: JSON with detailed broker state, job counts, configuration, and diagnostics

## Kubernetes Deployment Configuration

### Basic Configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: amps
  labels:
    app: amps
spec:
  replicas: 3
  selector:
    matchLabels:
      app: amps
  template:
    metadata:
      labels:
        app: amps
    spec:
      containers:
      - name: amps
        image: your-registry/amps:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: BROKER_DSN
          valueFrom:
            secretKeyRef:
              name: amps-config
              key: broker-dsn
        - name: MAX_CONCURRENCY
          value: "10"

        # Readiness Probe - Traffic routing
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          successThreshold: 1
          failureThreshold: 3

        # Liveness Probe - Container restart
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          successThreshold: 1
          failureThreshold: 3

        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

### Production Configuration with Advanced Settings

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: amps
  namespace: messaging
  labels:
    app: amps
    version: v1.0.0
spec:
  replicas: 5
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 2
  selector:
    matchLabels:
      app: amps
  template:
    metadata:
      labels:
        app: amps
        version: v1.0.0
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: amps
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000

      containers:
      - name: amps
        image: your-registry/amps:v1.0.0
        imagePullPolicy: IfNotPresent

        ports:
        - containerPort: 8080
          name: http
          protocol: TCP
        - containerPort: 9090
          name: metrics
          protocol: TCP

        env:
        - name: BROKER_DSN
          valueFrom:
            secretKeyRef:
              name: amps-secrets
              key: rabbitmq-url
        - name: WORKER_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: MAX_CONCURRENCY
          value: "15"
        - name: JOB_TIMEOUT
          value: "300s"
        - name: METRICS_ENABLED
          value: "true"
        - name: DEBUG
          value: "false"

        # Readiness Probe Configuration
        readinessProbe:
          httpGet:
            path: /readyz
            port: http
            scheme: HTTP
          initialDelaySeconds: 15
          periodSeconds: 5
          timeoutSeconds: 3
          successThreshold: 1
          failureThreshold: 2

        # Liveness Probe Configuration
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
            scheme: HTTP
          initialDelaySeconds: 60
          periodSeconds: 30
          timeoutSeconds: 10
          successThreshold: 1
          failureThreshold: 3

        # Startup Probe for slow-starting applications
        startupProbe:
          httpGet:
            path: /readyz
            port: http
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 30  # 30 * 5s = 150s max startup time

        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "1Gi"
            cpu: "1000m"

        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - ALL

        volumeMounts:
        - name: tmp
          mountPath: /tmp

      volumes:
      - name: tmp
        emptyDir: {}

      # Graceful shutdown
      terminationGracePeriodSeconds: 60

      # Node affinity for better distribution
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - amps
              topologyKey: kubernetes.io/hostname
```

## Service Configuration

```yaml
apiVersion: v1
kind: Service
metadata:
  name: amps-service
  namespace: messaging
  labels:
    app: amps
spec:
  type: ClusterIP
  ports:
  - name: http
    port: 8080
    targetPort: http
    protocol: TCP
  - name: metrics
    port: 9090
    targetPort: metrics
    protocol: TCP
  selector:
    app: amps
```

## ServiceMonitor for Prometheus (Optional)

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: amps-metrics
  namespace: messaging
  labels:
    app: amps
spec:
  selector:
    matchLabels:
      app: amps
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
  - port: http
    interval: 60s
    path: /health/detailed
    targetPort: http
```

## Recommended Probe Settings

### Readiness Probe Settings
- **initialDelaySeconds**: `10-15` - Allow time for basic initialization
- **periodSeconds**: `5` - Check frequently to route traffic quickly
- **timeoutSeconds**: `3` - Quick timeout for responsiveness
- **failureThreshold**: `2-3` - Be somewhat tolerant of temporary issues
- **successThreshold**: `1` - Single success is sufficient

### Liveness Probe Settings
- **initialDelaySeconds**: `30-60` - Allow time for full startup and connection establishment
- **periodSeconds**: `10-30` - Check less frequently than readiness
- **timeoutSeconds**: `5-10` - Allow more time for response
- **failureThreshold**: `3` - Be tolerant to avoid unnecessary restarts
- **successThreshold**: `1` - Single success is sufficient

### Startup Probe Settings (Kubernetes 1.18+)
- **initialDelaySeconds**: `5-10` - Start checking early
- **periodSeconds**: `5` - Check frequently during startup
- **failureThreshold**: `20-30` - Allow for slow broker connections
- **timeoutSeconds**: `3` - Keep timeouts short

## Health Check Behavior

### Readiness Probe Behavior
- **Ready**: Application initialized AND broker connected AND healthy
- **Not Ready**: Application initializing OR broker disconnected OR reconnection in progress
- **Traffic Impact**: Pod removed from load balancer when not ready

### Liveness Probe Behavior
- **Healthy**: Application responding AND (broker healthy OR reconnection in progress)
- **Unhealthy**: Application unresponsive OR broker permanently failed
- **Restart Trigger**: Only when application is completely unresponsive or broker cannot reconnect

### Startup Probe Behavior
- **Purpose**: Prevents liveness probe from killing slow-starting containers
- **Duration**: Allows up to `failureThreshold * periodSeconds` for startup
- **Recommendation**: Use when RabbitMQ connections might be slow to establish

## Common Scenarios

### RabbitMQ Restart
1. **Readiness**: Becomes "not ready" when connection lost
2. **Liveness**: Remains "healthy" during reconnection attempts
3. **Traffic**: Removed from load balancer until reconnected
4. **Result**: No container restart, automatic recovery

### AMPS Application Hang
1. **Readiness**: Health check times out
2. **Liveness**: Health check fails after failureThreshold
3. **Traffic**: Removed from load balancer
4. **Result**: Container restarted by Kubernetes

### Slow Startup
1. **Startup**: Allows extended time for initialization
2. **Readiness**: Waits for broker connection
3. **Liveness**: Disabled until startup probe succeeds
4. **Result**: No premature restarts

## Troubleshooting

### Common Issues

#### Probe Timeouts
**Symptoms**: `probe timeout` in events
**Causes**: Network latency, application overload
**Solutions**: Increase
 `timeoutSeconds`, check resource limits

#### Rapid Restarts
**Symptoms**: Container restarts frequently
**Causes**: Aggressive liveness probe settings
**Solutions**: Increase `failureThreshold`, `periodSeconds`, or `initialDelaySeconds`

#### Traffic Not Routing
**Symptoms**: No requests reaching pods
**Causes**: Readiness probe failing
**Solutions**: Check `/readyz` endpoint manually, verify broker connectivity

### Debug Commands

```bash
# Check pod status
kubectl get pods -l app=amps -o wide

# Check probe events
kubectl describe pod <amps-pod-name>

# Test health endpoints directly
kubectl port-forward pod/<amps-pod-name> 8080:8080
curl http://localhost:8080/readyz
curl http://localhost:8080/healthz
curl http://localhost:8080/health/detailed

# Check logs
kubectl logs <amps-pod-name> --tail=100

# Check service endpoints
kubectl get endpoints amps-service
```

### Health Check Response Examples

#### Healthy Response (`/readyz`)
```json
{
  "status": "ready",
  "timestamp": "2023-12-07T10:30:45Z",
  "uptime": "2h15m30s",
  "broker_type": "amqp",
  "connected": true,
  "job_count": 5
}
```

#### Unhealthy Response (`/healthz`)
```json
{
  "status": "unhealthy",
  "timestamp": "2023-12-07T10:30:45Z",
  "uptime": "45m12s",
  "broker_type": "amqp",
  "connected": false,
  "job_count": 0,
  "details": "broker connection failed - reconnection in progress"
}
```

#### Detailed Health Response (`/health/detailed`)
```json
{
  "status": "healthy",
  "timestamp": "2023-12-07T10:30:45Z",
  "uptime": "2h15m30s",
  "uptime_seconds": 8130,
  "ready": true,
  "broker_type": "amqp",
  "broker_healthy": true,
  "broker_details": {
    "connected": true,
    "running": true,
    "last_connected": "2023-12-07T08:15:15Z",
    "reconnect_count": 1,
    "consecutive_failures": 0,
    "connection_closed": false,
    "consume_channel_available": true,
    "publish_channel_available": true,
    "uptime_seconds": 8130
  },
  "jobs": {
    "count": 5,
    "max_concurrency": 15,
    "active_jobs": {
      "job-123": {
        "created": "2023-12-07T10:29:30Z",
        "age": "1m15s"
      }
    }
  },
  "config": {
    "worker_id": "amps-deployment-abc123",
    "broker_subject": "jobs",
    "job_timeout": 300000000000,
    "metrics_enabled": true
  }
}
```

## Best Practices

1. **Use Both Probes**: Always configure both readiness and liveness probes
2. **Conservative Liveness**: Set liveness probe thresholds high to avoid unnecessary restarts
3. **Responsive Readiness**: Set readiness probe to respond quickly to connection issues
4. **Resource Limits**: Ensure adequate CPU/memory for health check responsiveness
5. **Monitor Events**: Watch Kubernetes events for probe failures
6. **Test Scenarios**: Test behavior during RabbitMQ outages and application issues
7. **Gradual Rollouts**: Use rolling updates with proper probe settings
8. **Observability**: Monitor both health endpoints and application metrics

## Migration Guide

### From Basic Health Checks
1. Replace `/health` with `/healthz` for liveness
2. Add `/readyz` for readiness probe
3. Adjust probe timing based on recommendations
4. Test thoroughly in staging environment

### From External Health Checks
1. Remove external health check containers/sidecars
2. Configure native Kubernetes probes
3. Update monitoring systems to use new endpoints
4. Verify load balancer behavior

---

**Important**: These health checks are designed to work with the reconnection fixes implemented in AMPS. They will properly handle RabbitMQ connection issues without unnecessary container restarts while ensuring traffic is routed only to healthy instances.