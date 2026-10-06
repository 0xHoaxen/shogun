const LENGTH_BYTES = 8;

// bodyDigest is the SHA-256 the server binds into an approval for a message:
// each of the subject and the body, as UTF-8, preceded by its byte length as an
// 8-byte big-endian number. It must match hanko.BodyDigest in pkg/hanko exactly
// (a golden test there pins the format); an approval whose digest differs from
// the stored text is refused, so a text that changed since the owner read it
// cannot be approved by mistake.
export async function bodyDigest(subject: string, body: string): Promise<Uint8Array<ArrayBuffer>> {
  if (!globalThis.crypto?.subtle) {
    // Web Crypto exists only on secure origins (https and localhost).
    throw new Error("This page needs a secure connection to approve a draft.");
  }
  const encoder = new TextEncoder();
  const parts = [encoder.encode(subject), encoder.encode(body)];
  const data = new Uint8Array(parts.reduce((total, part) => total + LENGTH_BYTES + part.length, 0));
  const view = new DataView(data.buffer);
  let offset = 0;
  for (const part of parts) {
    view.setBigUint64(offset, BigInt(part.length)); // big-endian by default
    data.set(part, offset + LENGTH_BYTES);
    offset += LENGTH_BYTES + part.length;
  }
  return new Uint8Array(await crypto.subtle.digest("SHA-256", data));
}
