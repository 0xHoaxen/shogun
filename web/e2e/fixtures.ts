import {
  create,
  toJson,
  type DescMessage,
  type DescMethodServerStreaming,
  type DescMethodUnary,
  type MessageInitShape,
} from "@bufbuild/protobuf";
import { BinaryWriter, WireType } from "@bufbuild/protobuf/wire";
import type { Page } from "@playwright/test";

// RpcCall is one request the browser made to a mocked method.
export interface RpcCall {
  body: unknown;
  headers: Record<string, string>;
}

// HTTP statuses Connect uses for the error codes the specs need.
const HTTP_STATUS = {
  unauthenticated: 401,
  invalid_argument: 400,
  failed_precondition: 400,
  aborted: 409,
  unavailable: 503,
  internal: 500,
} as const;

export type ErrorCode = keyof typeof HTTP_STATUS;

function rpcPattern(method: DescMethodUnary): string {
  return `**/api/${method.parent.typeName}/${method.name}`;
}

// A response is either fixed or computed per call from the request body, so a
// test can change what a refetch returns after a mutation or answer a filter.
type Response<O extends DescMessage> =
  | MessageInitShape<O>
  | ((body: Record<string, unknown>) => MessageInitShape<O>);

// mockRpc answers a unary method with response, serialised by the generated
// schema, and returns the calls the page makes to it.
export async function mockRpc<I extends DescMessage, O extends DescMessage>(
  page: Page,
  method: DescMethodUnary<I, O>,
  response: Response<O>,
): Promise<RpcCall[]> {
  const calls: RpcCall[] = [];
  await page.route(rpcPattern(method), async (route) => {
    const request = route.request();
    const body = request.postDataJSON() as Record<string, unknown>;
    calls.push({ body, headers: request.headers() });
    const init = typeof response === "function" ? response(body) : response;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(toJson(method.output, create(method.output, init))),
    });
  });
  return calls;
}

// errorInfoDetail is the wire form of a google.rpc.ErrorInfo error detail with
// the given reason: the unpadded base64 of the serialised message.
function errorInfoDetail(reason: string) {
  const bytes = new BinaryWriter()
    .tag(1, WireType.LengthDelimited)
    .string(reason)
    .tag(2, WireType.LengthDelimited)
    .string("shogun")
    .finish();
  return {
    type: "google.rpc.ErrorInfo",
    value: Buffer.from(bytes).toString("base64").replace(/=+$/, ""),
  };
}

// mockRpcError answers a unary method with a Connect error. A reason adds the
// stable ErrorInfo.reason torii attaches to errors the owner can act on.
export async function mockRpcError<I extends DescMessage, O extends DescMessage>(
  page: Page,
  method: DescMethodUnary<I, O>,
  code: ErrorCode,
  message: string,
  reason?: string,
): Promise<RpcCall[]> {
  const calls: RpcCall[] = [];
  await page.route(rpcPattern(method), async (route) => {
    const request = route.request();
    calls.push({ body: request.postDataJSON(), headers: request.headers() });
    await route.fulfill({
      status: HTTP_STATUS[code],
      contentType: "application/json",
      body: JSON.stringify({ code, message, details: reason ? [errorInfoDetail(reason)] : [] }),
    });
  });
  return calls;
}

// Connect frames a streamed message as one flag byte and a four byte length;
// the flag 2 marks the closing frame, which carries the trailers.
const END_STREAM_FLAG = 2;

function frame(flags: number, payload: unknown): Buffer {
  const json = Buffer.from(JSON.stringify(payload));
  const header = Buffer.alloc(5);
  header.writeUInt8(flags, 0);
  header.writeUInt32BE(json.length, 1);
  return Buffer.concat([header, json]);
}

// A streaming request body is one frame; this reads its message.
function streamRequestBody(data: Buffer | null): Record<string, unknown> {
  if (!data || data.length <= 5) return {};
  return JSON.parse(data.subarray(5).toString()) as Record<string, unknown>;
}

// mockServerStream answers each call of a server-streaming method with the
// messages replies returns, then ends the stream. replies gets the request body
// and the number of the call, starting at 1, and may wait before it answers.
// Real streams stay open; a mocked one ends at once, so the page reconnects,
// which is what the specs on the page's reconnecting rely on.
export async function mockServerStream<I extends DescMessage, O extends DescMessage>(
  page: Page,
  method: DescMethodServerStreaming<I, O>,
  replies: (body: Record<string, unknown>, call: number) => Promise<MessageInitShape<O>[]> | MessageInitShape<O>[],
): Promise<RpcCall[]> {
  const calls: RpcCall[] = [];
  await page.route(`**/api/${method.parent.typeName}/${method.name}`, async (route) => {
    const request = route.request();
    const body = streamRequestBody(request.postDataBuffer());
    calls.push({ body, headers: request.headers() });
    const messages = await replies(body, calls.length);
    const frames = messages.map((init) => frame(0, toJson(method.output, create(method.output, init))));
    await route.fulfill({
      status: 200,
      contentType: "application/connect+json",
      body: Buffer.concat([...frames, frame(END_STREAM_FLAG, {})]),
    });
  });
  return calls;
}
