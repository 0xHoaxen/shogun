import { ConnectError } from "@connectrpc/connect";
import { BinaryReader, WireType } from "@bufbuild/protobuf/wire";

const ERROR_INFO_TYPE = "google.rpc.ErrorInfo";
const ERROR_INFO_REASON_FIELD = 1;

// Stable ErrorInfo.reason values the screens switch on.
export const REASON_JOB_INVALID_TRANSITION = "JOB_STATUS_INVALID_TRANSITION";
export const REASON_CONTACT_INVALID_TRANSITION = "CONTACT_STATUS_INVALID_TRANSITION";
export const REASON_VERSION_CONFLICT = "VERSION_CONFLICT";
export const REASON_BODY_HASH_MISMATCH = "BODY_HASH_MISMATCH";
export const REASON_RECIPIENT_REQUIRED = "RECIPIENT_REQUIRED";
export const REASON_SEND_REFUSED = "SEND_REFUSED";
export const REASON_SEND_FAILED = "SEND_FAILED";
export const REASON_SEND_UNAVAILABLE = "SEND_UNAVAILABLE";
export const REASON_SEND_STATUS_UNKNOWN = "SEND_STATUS_UNKNOWN";
export const REASON_INVALID_STATE = "INVALID_STATE";
export const REASON_INVALID_CODE = "INVALID_CODE";

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
