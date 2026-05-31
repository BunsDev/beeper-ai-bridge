#!/usr/bin/env node
import http from "node:http";
import { randomUUID } from "node:crypto";

const DEFAULT_MODEL = {
	id: "faux-1",
	name: "Faux Model",
	api: "openai-responses",
	provider: "faux",
	baseUrl: "http://127.0.0.1",
	reasoning: false,
	input: ["text", "image"],
	cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
	contextWindow: 128000,
	maxTokens: 16384,
};

const state = {
	models: [DEFAULT_MODEL],
	responses: [],
	callCount: 0,
	requests: [],
};

function usageFor(content) {
	const text = content
		.map((block) => {
			if (block.type === "text") return block.text ?? "";
			if (block.type === "thinking") return block.thinking ?? "";
			if (block.type === "toolCall") return `${block.name}:${JSON.stringify(block.arguments ?? {})}`;
			return JSON.stringify(block);
		})
		.join("\n");
	const output = Math.ceil(text.length / 4);
	return {
		input: 1,
		output,
		cacheRead: 0,
		cacheWrite: 0,
		totalTokens: 1 + output,
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
	};
}

function normalizeContent(content) {
	if (typeof content === "string") return [{ type: "text", text: content }];
	if (!Array.isArray(content)) return [];
	return content.map((block) => {
		if (typeof block === "string") return { type: "text", text: block };
		return block;
	});
}

function normalizeResponse(raw) {
	if (typeof raw === "string") {
		raw = { content: raw };
	}
	const content = normalizeContent(raw?.content ?? "");
	return {
		role: "assistant",
		content,
		api: raw?.api ?? "faux",
		provider: raw?.provider ?? "faux",
		model: raw?.model ?? DEFAULT_MODEL.id,
		usage: raw?.usage ?? usageFor(content),
		stopReason: raw?.stopReason ?? "stop",
		errorMessage: raw?.errorMessage,
		responseId: raw?.responseId ?? `resp_${randomUUID()}`,
		timestamp: raw?.timestamp ?? Date.now(),
	};
}

function nextResponse() {
	state.callCount++;
	const raw = state.responses.shift();
	if (raw === undefined) {
		return normalizeResponse({ content: [], stopReason: "error", errorMessage: "No more faux responses queued" });
	}
	return normalizeResponse(raw);
}

function redactHeaders(headers) {
	const redacted = { ...headers };
	for (const name of Object.keys(redacted)) {
		const lower = name.toLowerCase();
		if (lower === "authorization" || lower === "cookie" || lower === "set-cookie" || lower === "x-api-key" || lower.includes("token")) {
			redacted[name] = "<redacted>";
		}
	}
	return redacted;
}

function readBody(req) {
	return new Promise((resolve, reject) => {
		let body = "";
		req.setEncoding("utf8");
		req.on("data", (chunk) => {
			body += chunk;
		});
		req.on("end", () => {
			if (!body) {
				resolve(undefined);
				return;
			}
			try {
				resolve(JSON.parse(body));
			} catch (error) {
				reject(error);
			}
		});
		req.on("error", reject);
	});
}

function sendJSON(res, status, body) {
	res.writeHead(status, { "content-type": "application/json" });
	res.end(JSON.stringify(body));
}

function writeSSE(res, value) {
	res.write(`data: ${JSON.stringify(value)}\n\n`);
}

function startSSE(res) {
	res.writeHead(200, {
		"content-type": "text/event-stream",
		"cache-control": "no-cache, no-transform",
		connection: "keep-alive",
		"x-accel-buffering": "no",
	});
}

function chunks(text, size = 12) {
	if (!text) return [""];
	const out = [];
	for (let i = 0; i < text.length; i += size) {
		out.push(text.slice(i, i + size));
	}
	return out;
}

function bridgeEventStream(res, response) {
	startSSE(res);
	const partial = { ...response, content: [] };
	writeSSE(res, { type: "start", partial });
	for (let i = 0; i < response.content.length; i++) {
		const block = response.content[i];
		if (block.type === "thinking") {
			partial.content.push({ type: "thinking", thinking: "" });
			writeSSE(res, { type: "thinking_start", contentIndex: i, partial });
			for (const delta of chunks(block.thinking ?? "")) {
				partial.content[i].thinking += delta;
				writeSSE(res, { type: "thinking_delta", contentIndex: i, delta, partial });
			}
			writeSSE(res, { type: "thinking_end", contentIndex: i, content: block.thinking ?? "", partial });
		} else if (block.type === "toolCall") {
			partial.content.push({ type: "toolCall", id: block.id, name: block.name, arguments: {} });
			writeSSE(res, { type: "toolcall_start", contentIndex: i, partial });
			const args = JSON.stringify(block.arguments ?? {});
			for (const delta of chunks(args)) {
				writeSSE(res, { type: "toolcall_delta", contentIndex: i, delta, partial });
			}
			partial.content[i].arguments = block.arguments ?? {};
			writeSSE(res, {
				type: "toolcall_end",
				contentIndex: i,
				toolCall: { type: "toolCall", id: block.id, name: block.name, arguments: block.arguments ?? {} },
				partial,
			});
		} else {
			partial.content.push({ type: "text", text: "" });
			writeSSE(res, { type: "text_start", contentIndex: i, partial });
			for (const delta of chunks(block.text ?? "")) {
				partial.content[i].text += delta;
				writeSSE(res, { type: "text_delta", contentIndex: i, delta, partial });
			}
			writeSSE(res, { type: "text_end", contentIndex: i, content: block.text ?? "", partial });
		}
	}
	if (response.stopReason === "error" || response.stopReason === "aborted") {
		writeSSE(res, { type: "error", reason: response.stopReason, error: response });
	} else {
		writeSSE(res, { type: "done", reason: response.stopReason, message: response });
	}
	res.end();
}

function chatCompletionStream(res, request, response) {
	startSSE(res);
	const id = response.responseId ?? `chatcmpl_${randomUUID()}`;
	const model = request?.model ?? response.model ?? DEFAULT_MODEL.id;
	let toolIndex = 0;
	for (const block of response.content) {
		if (block.type === "thinking") {
			for (const delta of chunks(block.thinking ?? "")) {
				writeSSE(res, { id, object: "chat.completion.chunk", created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { reasoning_content: delta } }] });
			}
		} else if (block.type === "toolCall") {
			const index = toolIndex++;
			writeSSE(res, {
				id,
				object: "chat.completion.chunk",
				created: Math.floor(Date.now() / 1000),
				model,
				choices: [{ index: 0, delta: { tool_calls: [{ index, id: block.id, type: "function", function: { name: block.name, arguments: "" } }] } }],
			});
			for (const delta of chunks(JSON.stringify(block.arguments ?? {}))) {
				writeSSE(res, {
					id,
					object: "chat.completion.chunk",
					created: Math.floor(Date.now() / 1000),
					model,
					choices: [{ index: 0, delta: { tool_calls: [{ index, function: { arguments: delta } }] } }],
				});
			}
		} else {
			for (const delta of chunks(block.text ?? "")) {
				writeSSE(res, { id, object: "chat.completion.chunk", created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { content: delta } }] });
			}
		}
	}
	const finishReason = response.stopReason === "toolUse" || response.content.some((block) => block.type === "toolCall") ? "tool_calls" : "stop";
	writeSSE(res, {
		id,
		object: "chat.completion.chunk",
		created: Math.floor(Date.now() / 1000),
		model,
		choices: [{ index: 0, delta: {}, finish_reason: response.stopReason === "error" ? "error" : finishReason }],
		usage: { prompt_tokens: response.usage.input, completion_tokens: response.usage.output, total_tokens: response.usage.totalTokens },
	});
	res.write("data: [DONE]\n\n");
	res.end();
}

function responsesStream(res, request, response) {
	startSSE(res);
	const responseID = response.responseId ?? `resp_${randomUUID()}`;
	const model = request?.model ?? response.model ?? DEFAULT_MODEL.id;
	writeSSE(res, { type: "response.created", response: { id: responseID, model, status: "in_progress" } });
	for (let i = 0; i < response.content.length; i++) {
		const block = response.content[i];
		if (block.type === "thinking") {
			const itemID = `rs_${i}`;
			writeSSE(res, { type: "response.output_item.added", output_index: i, item: { id: itemID, type: "reasoning", summary: [] } });
			writeSSE(res, { type: "response.reasoning_summary_part.added", item_id: itemID, output_index: i, summary_index: 0, part: { type: "summary_text", text: "" } });
			for (const delta of chunks(block.thinking ?? "")) {
				writeSSE(res, { type: "response.reasoning_summary_text.delta", item_id: itemID, output_index: i, summary_index: 0, delta });
			}
			writeSSE(res, { type: "response.reasoning_summary_part.done", item_id: itemID, output_index: i, summary_index: 0, part: { type: "summary_text", text: block.thinking ?? "" } });
			writeSSE(res, { type: "response.output_item.done", output_index: i, item: { id: itemID, type: "reasoning", summary: [{ type: "summary_text", text: block.thinking ?? "" }] } });
		} else if (block.type === "toolCall") {
			const itemID = block.id?.includes("|") ? block.id.split("|")[1] : `fc_${i}`;
			const callID = block.id?.includes("|") ? block.id.split("|")[0] : (block.id ?? `call_${i}`);
			writeSSE(res, { type: "response.output_item.added", output_index: i, item: { id: itemID, type: "function_call", call_id: callID, name: block.name, arguments: "" } });
			const args = JSON.stringify(block.arguments ?? {});
			for (const delta of chunks(args)) {
				writeSSE(res, { type: "response.function_call_arguments.delta", item_id: itemID, output_index: i, delta });
			}
			writeSSE(res, { type: "response.function_call_arguments.done", item_id: itemID, output_index: i, arguments: args });
			writeSSE(res, { type: "response.output_item.done", output_index: i, item: { id: itemID, type: "function_call", call_id: callID, name: block.name, arguments: args } });
		} else {
			const itemID = `msg_${i}`;
			writeSSE(res, { type: "response.output_item.added", output_index: i, item: { id: itemID, type: "message", role: "assistant", content: [] } });
			writeSSE(res, { type: "response.content_part.added", item_id: itemID, output_index: i, content_index: 0, part: { type: "output_text", text: "" } });
			for (const delta of chunks(block.text ?? "")) {
				writeSSE(res, { type: "response.output_text.delta", item_id: itemID, output_index: i, content_index: 0, delta });
			}
			writeSSE(res, { type: "response.output_item.done", output_index: i, item: { id: itemID, type: "message", role: "assistant", content: [{ type: "output_text", text: block.text ?? "" }] } });
		}
	}
	if (response.stopReason === "error") {
		writeSSE(res, { type: "response.failed", response: { id: responseID, status: "failed", error: { message: response.errorMessage ?? "faux error" } } });
	} else {
		writeSSE(res, {
			type: "response.completed",
			response: {
				id: responseID,
				status: "completed",
				model,
				usage: {
					input_tokens: response.usage.input,
					output_tokens: response.usage.output,
					total_tokens: response.usage.totalTokens,
				},
			},
		});
	}
	res.write("data: [DONE]\n\n");
	res.end();
}

async function handle(req, res) {
	try {
		const url = new URL(req.url ?? "/", "http://localhost");
		if (req.method === "GET" && url.pathname === "/__faux/state") {
			sendJSON(res, 200, { callCount: state.callCount, pendingResponseCount: state.responses.length, requests: state.requests });
			return;
		}
		if (req.method === "POST" && url.pathname === "/__faux/reset") {
			const body = await readBody(req);
			state.responses = [];
			state.callCount = 0;
			state.requests = [];
			state.models = Array.isArray(body?.models) && body.models.length > 0 ? body.models : [DEFAULT_MODEL];
			sendJSON(res, 200, { ok: true });
			return;
		}
		if (req.method === "POST" && (url.pathname === "/__faux/responses" || url.pathname === "/__faux/responses/append")) {
			const body = await readBody(req);
			const responses = Array.isArray(body) ? body : (body?.responses ?? []);
			if (!Array.isArray(responses)) {
				sendJSON(res, 400, { error: "expected an array or {responses: []}" });
				return;
			}
			if (url.pathname.endsWith("/append")) {
				state.responses.push(...responses);
			} else {
				state.responses = [...responses];
			}
			sendJSON(res, 200, { pendingResponseCount: state.responses.length });
			return;
		}
		if (req.method === "GET" && url.pathname === "/v1/models") {
			sendJSON(res, 200, { object: "list", data: state.models.map((model) => ({ id: model.id, object: "model", owned_by: model.provider ?? "faux" })) });
			return;
		}
		if (req.method === "POST" && ["/api/stream", "/v1/chat/completions", "/v1/responses"].includes(url.pathname)) {
			const body = await readBody(req);
				state.requests.push({ method: req.method, path: url.pathname, headers: redactHeaders(req.headers), body });
			const response = nextResponse();
			if (url.pathname === "/api/stream") bridgeEventStream(res, response);
			if (url.pathname === "/v1/chat/completions") chatCompletionStream(res, body, response);
			if (url.pathname === "/v1/responses") responsesStream(res, body, response);
			return;
		}
		sendJSON(res, 404, { error: "not found" });
	} catch (error) {
		sendJSON(res, 500, { error: error instanceof Error ? error.message : String(error) });
	}
}

const portArgIndex = process.argv.indexOf("--port");
const port = portArgIndex >= 0 ? Number(process.argv[portArgIndex + 1]) : Number(process.env.PORT || 0);
const host = process.env.HOST || "127.0.0.1";
const server = http.createServer(handle);
server.listen(port, host, () => {
	const address = server.address();
	const url = `http://${address.address}:${address.port}`;
	console.log(JSON.stringify({ url }));
});
