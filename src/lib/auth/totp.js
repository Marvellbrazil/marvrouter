import crypto from "node:crypto";

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

export function base32Encode(buffer) {
  let bits = 0;
  let value = 0;
  let output = "";

  for (let i = 0; i < buffer.length; i++) {
    value = (value << 8) | buffer[i];
    bits += 8;

    while (bits >= 5) {
      output += BASE32_ALPHABET[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }

  if (bits > 0) {
    output += BASE32_ALPHABET[(value << (5 - bits)) & 31];
  }

  return output;
}

export function base32Decode(input) {
  const cleanInput = String(input)
    .toUpperCase()
    .replace(/=+$/, "")
    .replace(/\s+/g, "");
  let bits = 0;
  let value = 0;
  const bytes = [];

  for (let i = 0; i < cleanInput.length; i++) {
    const index = BASE32_ALPHABET.indexOf(cleanInput[i]);
    if (index === -1) {
      throw new Error(`Invalid Base32 character: ${cleanInput[i]}`);
    }
    value = (value << 5) | index;
    bits += 5;

    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 255);
      bits -= 8;
    }
  }

  return Buffer.from(bytes);
}

export function generateTotpSecret(length = 32) {
  const byteCount = Math.ceil((length * 5) / 8);
  const randomBytes = crypto.randomBytes(byteCount);
  return base32Encode(randomBytes).slice(0, length);
}

export function generateTotpUri(username, secret, issuer = "9Router") {
  const safeIssuer = String(issuer).trim() || "9Router";
  const safeUser = String(username).trim() || "admin";
  const label = `${encodeURIComponent(safeIssuer)}:${encodeURIComponent(safeUser)}`;
  const cleanSecret = String(secret).toUpperCase().replace(/\s+/g, "");

  return `otpauth://totp/${label}?secret=${cleanSecret}&issuer=${encodeURIComponent(safeIssuer)}&algorithm=SHA1&digits=6&period=30`;
}

export function generateTotpCode(secret, timeStep) {
  const step =
    timeStep !== undefined && timeStep !== null
      ? timeStep
      : Math.floor(Date.now() / 1000 / 30);
  const key = base32Decode(secret);
  const buffer = Buffer.alloc(8);
  buffer.writeBigInt64BE(BigInt(step));

  const hmac = crypto.createHmac("sha1", key);
  hmac.update(buffer);
  const digest = hmac.digest();

  const offset = digest[digest.length - 1] & 0x0f;
  const binary =
    ((digest[offset] & 0x7f) << 24) |
    ((digest[offset + 1] & 0xff) << 16) |
    ((digest[offset + 2] & 0xff) << 8) |
    (digest[offset + 3] & 0xff);

  const otp = binary % 1000000;
  return String(otp).padStart(6, "0");
}

export function verifyTotp(token, secret, window = 1) {
  if (!token || !secret) return false;

  const cleanToken = String(token).trim();
  if (!/^\d{6}$/.test(cleanToken)) return false;

  let keyBuffer;
  try {
    keyBuffer = base32Decode(secret);
    if (keyBuffer.length === 0) return false;
  } catch {
    return false;
  }

  const period = 30;
  const currentTimeStep = Math.floor(Date.now() / 1000 / period);

  for (let offset = -window; offset <= window; offset++) {
    const timeStep = currentTimeStep + offset;
    const generated = generateTotpCode(secret, timeStep);

    if (
      generated.length === cleanToken.length &&
      crypto.timingSafeEqual(Buffer.from(generated), Buffer.from(cleanToken))
    ) {
      return true;
    }
  }

  return false;
}

export function generateRecoveryCodes(count = 8) {
  const chars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ";
  const codes = [];

  for (let i = 0; i < count; i++) {
    const bytes = crypto.randomBytes(8);
    let code = "";
    for (let j = 0; j < 8; j++) {
      code += chars[bytes[j] % chars.length];
    }
    codes.push(code);
  }

  return codes;
}
