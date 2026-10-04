import {
  create,
  toJson,
  type DescMessage,
  type DescMethodUnary,
  type MessageInitShape,
} from "@bufbuild/protobuf";
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

// mockRpc answers a unary method with response, serialised by the generated
// schema, and returns the calls the page makes to it.
export async function mockRpc<I extends DescMessage, O extends DescMessage>(
  page: Page,
  method: DescMethodUnary<I, O>,
  response: MessageInitShape<O>,
): Promise<RpcCall[]> {
  const calls: RpcCall[] = [];
  await page.route(rpcPattern(method), async (route) => {
    const request = route.request();
    calls.push({ body: request.postDataJSON(), headers: request.headers() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(toJson(method.output, create(method.output, response))),
    });
  });
  return calls;
}

// mockRpcError answers a unary method with a Connect error.
export async function mockRpcError<I extends DescMessage, O extends DescMessage>(
  page: Page,
  method: DescMethodUnary<I, O>,
  code: ErrorCode,
  message: string,
): Promise<RpcCall[]> {
  const calls: RpcCall[] = [];
  await page.route(rpcPattern(method), async (route) => {
    const request = route.request();
    calls.push({ body: request.postDataJSON(), headers: request.headers() });
    await route.fulfill({
      status: HTTP_STATUS[code],
      contentType: "application/json",
      body: JSON.stringify({ code, message }),
    });
  });
  return calls;
}
