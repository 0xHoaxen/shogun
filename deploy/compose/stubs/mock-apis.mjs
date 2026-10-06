// Stand-in for Anthropic and Gmail in the end-to-end stack, so fude and tsubame
// run their real code against a server that never leaves the machine.
// No dependencies: `node mock-apis.mjs`.
//
// Anthropic:  POST /v1/messages/count_tokens, POST /v1/messages
// Google:     POST /token, GET /gmail/v1/users/me/{profile,messages,history},
//             GET /gmail/v1/users/me/messages/:id, POST .../messages/send
// Test only:  GET /_e2e/sent lists what was sent; DELETE /_e2e/sent clears it.
import { createServer } from "node:http";

const PORT = Number(process.env.PORT ?? 8090);
const MAILBOX = process.env.MOCK_GMAIL_ADDRESS ?? "owner@gmail.example";

const sent = [];
let drafts = 0;

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function send(res, status, body) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

// parseMime splits a raw message into lower-cased headers and its body.
function parseMime(raw) {
  const text = raw.replaceAll("\r\n", "\n");
  const cut = text.indexOf("\n\n");
  const head = cut < 0 ? text : text.slice(0, cut);
  const body = cut < 0 ? "" : text.slice(cut + 2);
  const headers = {};
  for (const line of head.split("\n")) {
    const colon = line.indexOf(":");
    if (colon > 0 && !/^\s/.test(line)) {
      headers[line.slice(0, colon).trim().toLowerCase()] = line.slice(colon + 1).trim();
    }
  }
  return { headers, body };
}

// Each call writes a different draft, so a regenerate makes a new version.
function nextDraft() {
  drafts += 1;
  return `Subject: Hello from draft ${drafts}\n\nHi there,\n\nThis is generated text number ${drafts}.\n\nBest regards`;
}

async function handle(req, res) {
  const url = new URL(req.url, "http://stub");
  const path = url.pathname;
  const key = `${req.method} ${path}`;

  if (key === "GET /healthz") return send(res, 200, { ok: true });

  if (key === "POST /v1/messages/count_tokens") {
    await readBody(req);
    return send(res, 200, { input_tokens: 120 });
  }
  if (key === "POST /v1/messages") {
    const body = JSON.parse((await readBody(req)) || "{}");
    return send(res, 200, {
      id: `msg_${drafts + 1}`,
      type: "message",
      role: "assistant",
      model: body.model ?? "stub",
      content: [{ type: "text", text: nextDraft() }],
      stop_reason: "end_turn",
      stop_sequence: null,
      usage: { input_tokens: 120, output_tokens: 60, cache_creation_input_tokens: 0, cache_read_input_tokens: 0 },
    });
  }

  if (key === "POST /token") {
    await readBody(req);
    return send(res, 200, {
      access_token: "stub-access",
      refresh_token: "stub-refresh",
      token_type: "Bearer",
      expires_in: 3600,
    });
  }
  if (key === "GET /gmail/v1/users/me/profile") {
    return send(res, 200, { emailAddress: MAILBOX, historyId: "1" });
  }
  if (key === "GET /gmail/v1/users/me/messages") return send(res, 200, {});
  if (key === "GET /gmail/v1/users/me/history") return send(res, 200, { historyId: "1" });
  if (key === "POST /gmail/v1/users/me/messages/send") {
    const { raw } = JSON.parse(await readBody(req));
    const message = parseMime(Buffer.from(raw, "base64url").toString("utf8"));
    const id = `sent-${sent.length + 1}`;
    sent.push({ id, ...message });
    return send(res, 200, { id, threadId: `thread-${id}` });
  }

  if (key === "GET /_e2e/sent") return send(res, 200, sent);
  if (key === "DELETE /_e2e/sent") {
    sent.length = 0;
    return send(res, 200, {});
  }

  return send(res, 404, { error: { status: "NOT_FOUND", message: key } });
}

createServer((req, res) => {
  handle(req, res).catch((err) => {
    console.error(err);
    send(res, 500, { error: { status: "INTERNAL", message: String(err) } });
  });
}).listen(PORT, () => console.log(`mock-apis listening on :${PORT}`));
