import { ConnectError } from "@connectrpc/connect";
import { BinaryReader, WireType } from "@bufbuild/protobuf/wire";

const ERROR_INFO_TYPE = "google.rpc.ErrorInfo";
const ERROR_INFO_REASON_FIELD = 1;

// Stable ErrorInfo.reason values the screens switch on.
export const REASON_JOB_INVALID_TRANSITION = "JOB_STATUS_INVALID_TRANSITION";
export const REASON_CONTACT_INVALID_TRANSITION = "CONTACT_STATUS_INVALID_TRANSITION";
export const REASON_VERSION_CONFLICT = "VERSION_CONFLICT";

function decodeReason(bytes: Uint8Array): string {
  const reader = new BinaryReader(bytes);
  while (reader.pos < reader.len) {
    const [field, wireType] = reader.tag();
    if (field === ERROR_INFO_REASON_FIELD && wireType === WireType.LengthDelimited) {
      return reader.string();
    }
    reader.skip(wireType);
  }
  return "";
}

// reasonOf returns the stable ErrorInfo.reason torii attached to an error, or
// "" when there is none. Clients switch on it and never parse the message.
export function reasonOf(error: unknown): string {
  const connectError = ConnectError.from(error);
  for (const detail of connectError.details) {
    if ("type" in detail && detail.type === ERROR_INFO_TYPE) {
      return decodeReason(detail.value);
    }
  }
  return "";
}
