# QoS-Based Concurrency Control Demo

This example demonstrates how AMPS now uses RabbitMQ QoS to control message concurrency instead of manual start/stop logic.

## Setup

1. Start RabbitMQ:
```bash
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management
```

2. Configure AMPS with concurrency limit:
```bash
export BROKER_HOST="amqp://localhost:5672"
export BROKER_SUBJECT="demo.jobs"
export MAX_CONCURRENCY=5
export WORKLOAD_ADDRESS="http://localhost:8080"
```

3. Start AMPS:
```bash
./amps
```

## How It Works

### Before (Manual Control)
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Message   │    │    AMPS     │    │  RabbitMQ   │
│  Received   │ -> │ Job Count=5 │ -> │   STOP      │
└─────────────┘    └─────────────┘    │ Consumer    │
                                      └─────────────┘

┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│    Job      │    │    AMPS     │    │   START     │
│ Completed   │ -> │ Job Count=4 │ -> │ Consumer    │
└─────────────┘    └─────────────┘    └─────────────┘
```

### After (QoS Control)
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│  RabbitMQ   │    │    AMPS     │    │  Workload   │
│ Prefetch=5  │ -> │ Always On   │ -> │ Processing  │
│Messages Max │    │ Consumer    │    │  Max 5 Jobs │
└─────────────┘    └─────────────┘    └─────────────┘
```

## Testing Concurrency

Send 10 messages to test concurrency limiting:

```bash
for i in {1..10}; do
  curl -X POST http://localhost:4000/publish \
    -H "Content-Type: application/json" \
    -d '{
      "specversion": "1.0",
      "type": "demo.job",
      "source": "test",
      "id": "job-'$i'",
      "data": {"task": "process", "id": '$i'}
    }'
  echo "Sent job $i"
done
```

## Verification

1. **Check RabbitMQ Management UI** (http://localhost:15672):
   - Login with guest/guest
   - Navigate to Queues tab
   - Click on your queue
   - See "Consumer prefetch" = 5

2. **Monitor AMPS Metrics** (http://localhost:9090/metrics):
   ```
   # Only 5 messages will be processing concurrently
   amps_messages_count 5
   ```

3. **Check Logs**:
   - No "Max job concurrency reached, stopping broker" messages
   - No "starting broker" messages
   - Consumer runs continuously

## Key Differences

### Configuration
- **Same**: `MAX_CONCURRENCY=5` environment variable
- **New**: RabbitMQ QoS prefetch count automatically set to 5

### Behavior
- **Before**: Consumer stops when 5 jobs active, starts when < 5
- **After**: Consumer always running, RabbitMQ limits to 5 unacked messages

### Performance
- **Before**: Restart latency on every concurrency change
- **After**: Zero latency, immediate message delivery when slot available

### Code Complexity
- **Before**: ~50 lines of start/stop logic across 4 functions
- **After**: Single QoS setting, RabbitMQ handles the rest

## Troubleshooting

### Issue: More than MAX_CONCURRENCY jobs processing
**Cause**: QoS not set correctly
**Check**: RabbitMQ management UI shows correct prefetch count
**Fix**: Verify `broker.config.MaxConcurrency` value in logs

### Issue: No messages being delivered  
**Cause**: All consumers busy with unacked messages
**Check**: `amps_messages_count` metric equals `MAX_CONCURRENCY`
**Expected**: Normal behavior - RabbitMQ waiting for acks

### Issue: Consumer not receiving messages
**Cause**: Connection or QoS setup failure  
**Check**: AMPS logs for QoS errors
**Fix**: Check RabbitMQ connection and permissions

## Migration Benefits Demonstrated

1. **Simplified Code**: No manual start/stop logic
2. **Better Performance**: Zero restart overhead  
3. **Automatic Flow Control**: RabbitMQ handles concurrency
4. **Fewer Race Conditions**: No manual state management
5. **Native Integration**: Uses RabbitMQ as designed

This demonstrates how leveraging RabbitMQ's built-in QoS mechanism provides more reliable and efficient concurrency control than manual consumer management.