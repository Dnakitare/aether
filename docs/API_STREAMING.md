# API Streaming Guide

**Status**: Beta v0.2.0 (Week 4, Day 27-28)
**Feature**: Real-time data streaming via Server-Sent Events (SSE)

---

## 🎯 Overview

The Aether API supports real-time streaming of agent logs using Server-Sent Events (SSE). This allows clients to receive log updates in real-time without polling.

---

## 📋 Table of Contents

- [Quick Start](#quick-start)
- [Streaming Endpoints](#streaming-endpoints)
- [SSE Protocol](#sse-protocol)
- [Client Examples](#client-examples)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)

---

## 🚀 Quick Start

### Basic Streaming Request

```bash
curl -N -H "Accept: text/event-stream" \
  "http://localhost:8080/v1/agents/agent-001/logs?follow=true"
```

### Using JavaScript EventSource

```javascript
const eventSource = new EventSource(
  'http://localhost:8080/v1/agents/agent-001/logs?follow=true'
);

eventSource.onmessage = (event) => {
  console.log('Log:', event.data);
};

eventSource.onerror = (error) => {
  console.error('Stream error:', error);
  eventSource.close();
};
```

---

## 📡 Streaming Endpoints

### GET /v1/agents/{id}/logs

Streams agent logs in real-time.

**Query Parameters:**
- `follow` (boolean): Enable streaming mode (required for SSE)

**Headers:**
- `Accept: text/event-stream` (required for SSE streaming)
- `Authorization: Bearer <token>` (if auth is enabled)

**Response Format:**

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

data: 2026-02-15T10:30:00Z [INFO] Agent started

data: 2026-02-15T10:30:01Z [INFO] Processing request

data: 2026-02-15T10:30:02Z [INFO] Task completed
```

**Response Codes:**
- `200 OK`: Stream established successfully
- `401 Unauthorized`: Missing or invalid authentication
- `403 Forbidden`: Access denied to this agent
- `404 Not Found`: Agent does not exist
- `500 Internal Server Error`: Failed to retrieve logs

---

## 🔌 SSE Protocol

### Event Format

Server-Sent Events use a simple text-based format:

```
data: <message>\n\n
```

Each event consists of:
1. `data:` prefix
2. The message content
3. Two newline characters (`\n\n`)

### Connection Management

**Client Responsibilities:**
- Maintain persistent HTTP connection
- Handle automatic reconnection
- Process events in real-time

**Server Responsibilities:**
- Keep connection alive
- Flush data immediately after each event
- Detect client disconnection
- Clean up resources on disconnect

### Heartbeats

To keep connections alive through proxies and load balancers:

```
data:

```

Empty data events can serve as heartbeats (not currently implemented but can be added if needed).

---

## 💻 Client Examples

### JavaScript (Browser)

```javascript
// Using EventSource API
const stream = new EventSource(
  '/v1/agents/agent-001/logs?follow=true',
  {
    headers: {
      'Authorization': 'Bearer ' + token
    }
  }
);

stream.addEventListener('message', (e) => {
  const logLine = e.data;
  appendToLogViewer(logLine);
});

stream.addEventListener('error', (e) => {
  if (e.readyState === EventSource.CLOSED) {
    console.log('Stream closed by server');
  } else {
    console.error('Stream error', e);
  }
});

// Close when done
function cleanup() {
  stream.close();
}
```

### Python

```python
import requests
import sseclient

url = 'http://localhost:8080/v1/agents/agent-001/logs?follow=true'
headers = {
    'Accept': 'text/event-stream',
    'Authorization': 'Bearer ' + token
}

response = requests.get(url, headers=headers, stream=True)
client = sseclient.SSEClient(response)

for event in client.events():
    print('Log:', event.data)
```

### Go

```go
package main

import (
    "bufio"
    "fmt"
    "net/http"
    "strings"
)

func streamLogs(agentID string) error {
    url := fmt.Sprintf("http://localhost:8080/v1/agents/%s/logs?follow=true", agentID)

    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return err
    }

    req.Header.Set("Accept", "text/event-stream")
    req.Header.Set("Authorization", "Bearer "+token)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()

        if strings.HasPrefix(line, "data: ") {
            logLine := strings.TrimPrefix(line, "data: ")
            fmt.Println("Log:", logLine)
        }
    }

    return scanner.Err()
}
```

### cURL

```bash
# Stream logs with follow mode
curl -N -H "Accept: text/event-stream" \
  -H "Authorization: Bearer ${TOKEN}" \
  "http://localhost:8080/v1/agents/agent-001/logs?follow=true"

# The -N flag disables buffering for real-time output
```

---

## 🚨 Error Handling

### Connection Errors

**Client Disconnection:**
```go
// Server detects disconnection via context
select {
case <-r.Context().Done():
    return // Client disconnected
default:
    // Continue streaming
}
```

**Network Timeouts:**
- SSE connections can be long-lived
- Configure appropriate timeouts on load balancers
- Implement reconnection logic in clients

### Reconnection Strategy

```javascript
class ReconnectingEventSource {
  constructor(url, options = {}) {
    this.url = url;
    this.options = options;
    this.reconnectDelay = 1000; // Start with 1 second
    this.maxReconnectDelay = 30000; // Max 30 seconds
    this.connect();
  }

  connect() {
    this.eventSource = new EventSource(this.url, this.options);

    this.eventSource.onmessage = (e) => {
      this.reconnectDelay = 1000; // Reset on success
      this.onMessage(e);
    };

    this.eventSource.onerror = (e) => {
      this.eventSource.close();

      // Exponential backoff
      setTimeout(() => {
        this.reconnectDelay = Math.min(
          this.reconnectDelay * 2,
          this.maxReconnectDelay
        );
        this.connect();
      }, this.reconnectDelay);

      this.onError(e);
    };
  }

  onMessage(event) {
    // Override in implementation
    console.log(event.data);
  }

  onError(error) {
    // Override in implementation
    console.error(error);
  }

  close() {
    if (this.eventSource) {
      this.eventSource.close();
    }
  }
}

// Usage
const stream = new ReconnectingEventSource('/v1/agents/agent-001/logs?follow=true');
stream.onMessage = (event) => {
  appendLog(event.data);
};
```

---

## ✅ Best Practices

### 1. Set Appropriate Timeouts

```javascript
// Client-side timeout
const timeout = setTimeout(() => {
  eventSource.close();
  console.log('Stream timeout after 5 minutes');
}, 5 * 60 * 1000);

eventSource.onmessage = (e) => {
  clearTimeout(timeout);
  // Process message
  // Reset timeout if needed
};
```

### 2. Handle Backpressure

If the client can't keep up with events:

```javascript
let buffer = [];
let processing = false;

eventSource.onmessage = (e) => {
  buffer.push(e.data);

  if (!processing) {
    processBuffer();
  }
};

async function processBuffer() {
  processing = true;

  while (buffer.length > 0) {
    const item = buffer.shift();
    await processLogLine(item);
  }

  processing = false;
}
```

### 3. Monitor Connection Health

```javascript
let lastEventTime = Date.now();

eventSource.onmessage = (e) => {
  lastEventTime = Date.now();
  // Process event
};

// Check for stale connections
setInterval(() => {
  const timeSinceLastEvent = Date.now() - lastEventTime;

  if (timeSinceLastEvent > 60000) { // 1 minute
    console.warn('No events received for 1 minute');
    eventSource.close();
    // Reconnect
  }
}, 10000);
```

### 4. Clean Up Resources

```javascript
// Always close streams when component unmounts
useEffect(() => {
  const eventSource = new EventSource(url);

  // Event handlers...

  return () => {
    eventSource.close();
  };
}, [url]);
```

### 5. Use Connection Pooling

For multiple streams, reuse connections:

```javascript
class StreamManager {
  constructor() {
    this.streams = new Map();
  }

  subscribe(agentID, callback) {
    let stream = this.streams.get(agentID);

    if (!stream) {
      stream = new EventSource(`/v1/agents/${agentID}/logs?follow=true`);
      stream.callbacks = new Set();

      stream.onmessage = (e) => {
        stream.callbacks.forEach(cb => cb(e.data));
      };

      this.streams.set(agentID, stream);
    }

    stream.callbacks.add(callback);

    return () => {
      stream.callbacks.delete(callback);

      if (stream.callbacks.size === 0) {
        stream.close();
        this.streams.delete(agentID);
      }
    };
  }
}
```

---

## 🔍 Debugging

### Enable Verbose Logging

```bash
# Server-side (Go)
export AETHER_LOG_LEVEL=debug
```

### Monitor Connection State

```javascript
eventSource.addEventListener('open', () => {
  console.log('Stream connected');
});

eventSource.addEventListener('error', (e) => {
  console.log('Stream error', {
    readyState: e.target.readyState,
    // EventSource.CONNECTING = 0
    // EventSource.OPEN = 1
    // EventSource.CLOSED = 2
  });
});
```

### Test with cURL

```bash
# Verify SSE format
curl -N -H "Accept: text/event-stream" \
  "http://localhost:8080/v1/agents/agent-001/logs?follow=true" | \
  hexdump -C
```

---

## 🔗 Related Documentation

- [API Reference](./api/API_REFERENCE.md)
- [Agent Management](./AGENT_MANAGEMENT.md)
- [Error Handling](../internal/api/errors.go)

---

## 📊 Performance Considerations

### Server Resources

- Each streaming connection holds a goroutine
- Monitor open connections with `/metrics`
- Configure appropriate limits in production

### Network Bandwidth

- Logs can generate significant data
- Consider filtering or sampling for high-volume agents
- Implement client-side buffering

### Load Balancer Configuration

Many load balancers need special configuration for SSE:

**nginx:**
```nginx
location /v1/agents/ {
    proxy_pass http://backend;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
}
```

**HAProxy:**
```
backend api
    option http-server-close
    timeout tunnel 1h
```

---

**Last Updated**: February 15, 2026
**Version**: Beta v0.2.0
**Status**: Beta
