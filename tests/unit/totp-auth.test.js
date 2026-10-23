import { describe, it, expect } from "vitest";
import {
  generateTotpSecret,
  generateTotpUri,
  generateTotpCode,
  verifyTotp,
  generateRecoveryCodes,
} from "../../src/lib/auth/totp.js";

describe("TOTP 2FA Service", () => {
  it("generates a valid 32-character base32 secret", () => {
    const secret = generateTotpSecret();
    expect(secret).toHaveLength(32);
    expect(/^[A-Z2-7]+$/.test(secret)).toBe(true);
  });

  it("generates standard otpauth URI", () => {
    const secret = generateTotpSecret();
    const uri = generateTotpUri("admin", secret, "9Router");
    expect(uri.startsWith("otpauth://totp/9Router:admin")).toBe(true);
    expect(uri).toContain(`secret=${secret}`);
    expect(uri).toContain("issuer=9Router");
  });

  it("generates and verifies 6-digit TOTP code accurately", () => {
    const secret = generateTotpSecret();
    const code = generateTotpCode(secret);
    expect(code).toHaveLength(6);
    expect(/^\d{6}$/.test(code)).toBe(true);
    expect(verifyTotp(code, secret)).toBe(true);
  });

  it("rejects invalid TOTP codes", () => {
    const secret = generateTotpSecret();
    expect(verifyTotp("000000", secret)).toBe(false);
    expect(verifyTotp("invalid", secret)).toBe(false);
    expect(verifyTotp("", secret)).toBe(false);
  });

  it("generates distinct 8-character recovery codes", () => {
    const codes = generateRecoveryCodes(8);
    expect(codes).toHaveLength(8);
    codes.forEach((c) => {
      expect(c).toHaveLength(8);
    });
    const unique = new Set(codes);
    expect(unique.size).toBe(8);
  });
});
