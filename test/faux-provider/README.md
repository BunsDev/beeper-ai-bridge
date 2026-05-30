# Faux Provider Server

Standalone local provider for ai-bridge tests and manual smoke checks.

Start it with:

```sh
node test/faux-provider/server.mjs --port 0
```

The server prints a JSON object with its URL:

```json
{"url":"http://127.0.0.1:52032"}
```

Queue responses:

```sh
curl -s -X POST "$URL/__faux/responses" \
  -H 'content-type: application/json' \
  --data '[{"content":"hello"}]'
```

Useful endpoints:

- `GET /v1/models`
- `POST /v1/responses`
- `POST /v1/chat/completions`
- `POST /api/stream`
- `POST /__faux/reset`
- `POST /__faux/responses`
- `POST /__faux/responses/append`
- `GET /__faux/state`

Response content blocks mirror `pkg/ai` blocks:

```json
[
  {
    "content": [
      { "type": "thinking", "thinking": "plan" },
      { "type": "text", "text": "hello" },
      { "type": "toolCall", "id": "call_1", "name": "echo", "arguments": { "text": "hi" } }
    ],
    "stopReason": "toolUse"
  }
]
```
