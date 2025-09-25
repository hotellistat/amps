# AMQP Resilience Improvements

This document describes the improvements made to AMPS to ensure graceful recovery when RabbitMQ (AMQP broker) restarts or becomes temporarily unavailable.

## Problem Statement

Previously, AMPS had several issues that prevented graceful recovery from RabbitMQ restarts:

1. **Fatal process termination**: When channel creation failed during reconnection, the process would call `os.Exit(1)`, killing the entire container
2. **No retry logic for publishing**: Operations like `Evacuate()` and `PublishMessage()` would fail silently or panic if connections were unavailable
3. **Brittle connection handling**: Various operations didn't properly check connection status before attempting to use channels
4. **Poor error visibility**: Limited logging made it difficult to diagnose connection issues

## Solution Overview

The AMQP broker implementation has been enhanced with robust reconnection logic, proper error handling, and comprehensive retry mechanisms.

### Key Improvements

#### 1. Exponential Backoff for Connection Retries

- **Before**: Fixed 1-second delays between connection attempts
- **After**: Exponential backoff starting at 1 second, capping at 60 seconds
- **Benefit**: Reduces load on RabbitMQ during extended outages while ensuring quick recovery

```go
backoff := time.Duration(math.Min(float64(baseBackoff)*math.Pow(2, float64(attempt)), float64(maxBackoff)))
```

#### 2. Resilient Channel Creation

- **Before**: `os.Exit(1)` on channel creation failure
- **After**: Retry logic with graceful fallback to reconnection
- **Benefit**: Container stays alive and continues attempting to recover

#### 3. Connection State Management

- **Before**: Basic connection tracking
- **After**: Thread-safe connection state with proper mutex protection
- **Benefit**: Prevents race conditions and ensures consistent state

```go
type AMQPBroker struct {
    // ... existing fields
    connMutex      *sync.RWMutex
    shutdownChan   chan bool
    isShuttingDown bool
}
```

#### 4. Publish Operations with Retry Logic

- **Before**: Single attempt publishing that could fail silently
- **After**: `publishWithRetry()` function with configurable retry attempts
- **Benefit**: Critical operations like job evacuation are more reliable

```go
func (broker *AMQPBroker) publishWithRetry(routingKey string, body []byte, maxRetries int) error
```

#### 5. Enhanced Health Checks

- **Before**: Simple connected flag check
- **After**: Comprehensive health check including connection state validation
- **Benefit**: More accurate health reporting for container orchestration

```go
func (broker *AMQPBroker) Healthy() bool {
    broker.connMutex.RLock()
    connected := broker.connected
    connection := broker.connection
    broker.connMutex.RUnlock()
    
    return connected && connection != nil && !connection.IsClosed()
}
```

#### 6. Graceful Shutdown Handling

- **Before**: Basic connection closure
- **After**: Coordinated shutdown with proper resource cleanup
- **Benefit**: Prevents resource leaks and ensures clean container termination

## Recovery Behavior

### Automatic Reconnection Flow

1. **Connection Loss Detection**: RabbitMQ connection drops are detected via `connection.NotifyClose()`
2. **State Reset**: All channels are properly closed and connection state is reset
3. **Reconnection Attempt**: New connection established with exponential backoff
4. **Channel Recreation**: Consumer and publisher channels are recreated with retry logic
5. **Service Resumption**: Message consumption automatically resumes

### Error Scenarios Handled

- **RabbitMQ server restart**: Full reconnection cycle
- **Network partitions**: Automatic retry with backoff
- **Channel-level errors**: Channel recreation without connection reset
- **Publishing failures**: Retry logic for critical operations
- **Graceful shutdown**: Clean resource disposal

## Monitoring and Observability

### Log Messages

The improved implementation provides detailed logging:

- `[AMPS] connecting to <uri>` - Connection attempt started
- `[AMPS] successfully connected to AMQP broker` - Connection established
- `[AMPS] successfully created AMQP channels` - Channels ready
- `[AMPS] dial exception: <error> - retrying in <duration>` - Connection retry with backoff
- `[AMPS] could not create consumer/publisher channel, attempt <n>: <error>` - Channel creation issues

### Health Check Endpoint

The `/healthz` endpoint now provides accurate connection status:
- Returns `200 OK` when broker is healthy and connected
- Returns `503 Service Unavailable` when connection issues exist

### Sentry Integration

All connection errors are properly reported to Sentry with appropriate context tags:
- `goroutine: amqpConnect` - Connection establishment errors
- `goroutine: amqpConnectRoutine` - Reconnection loop errors
- `goroutine: messageConsumer` - Message processing errors

## Configuration

No additional configuration is required. The resilience improvements use sensible defaults:

- **Base reconnection delay**: 1 second
- **Maximum reconnection delay**: 60 seconds
- **Channel creation retries**: 5 attempts
- **Publishing retries**: 3 attempts
- **Initial connection timeout**: 30 seconds

## Testing

### Integration Tests

New integration tests verify the resilience improvements:

- `TestAMQPReconnection`: Validates basic connection recovery
- `TestAMQPHealthCheck`: Verifies health check accuracy

### Manual Testing

To test reconnection behavior:

1. Start AMPS with RabbitMQ running
2. Stop RabbitMQ container: `docker stop <rabbitmq-container>`
3. Observe logs showing reconnection attempts
4. Start RabbitMQ container: `docker start <rabbitmq-container>`
5. Verify AMPS reconnects and resumes operation

## Backward Compatibility

All changes are backward compatible:
- No configuration changes required
- All existing broker interface methods unchanged
- Existing functionality preserved

## Performance Considerations

- **Memory usage**: Minimal increase due to additional state tracking
- **CPU usage**: Negligible impact during normal operation
- **Network usage**: Reduced during outages due to exponential backoff
- **Latency**: No impact during normal operation; faster recovery from outages

## Future Enhancements

Potential improvements for future releases:

1. **Configurable retry parameters**: Allow tuning of backoff and retry limits
2. **Circuit breaker pattern**: Temporarily disable problematic operations
3. **Connection pooling**: Multiple connections for high-throughput scenarios
4. **Metrics exposure**: Prometheus metrics for connection events
5. **Dead letter queue handling**: Enhanced error message processing

## Migration Guide

No migration steps required. The improvements are automatically active after deploying the updated AMPS version.

For containerized deployments, ensure:
- Health check timeouts account for potential reconnection delays
- Readiness probes use the `/healthz` endpoint
- Liveness probes have sufficient timeout for reconnection cycles