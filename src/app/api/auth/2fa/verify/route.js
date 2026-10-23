import { NextResponse } from "next/server";
import { cookies } from "next/headers";
import bcrypt from "bcryptjs";
import { updateSettings } from "@/lib/localDb";
import { getDashboardAuthSession } from "@/lib/auth/dashboardSession";
import { verifyTotp, generateRecoveryCodes } from "@/lib/auth/totp";

export async function POST(request) {
  try {
    const cookieStore = await cookies();
    const session = await getDashboardAuthSession(
      cookieStore.get("auth_token")?.value,
    );
    if (!session) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const body = await request.json();
    const { secret, code } = body;

    if (
      !secret ||
      typeof secret !== "string" ||
      !code ||
      typeof code !== "string"
    ) {
      return NextResponse.json(
        { error: "Secret and 6-digit code are required" },
        { status: 400 },
      );
    }

    const isValid = verifyTotp(code, secret);
    if (!isValid) {
      return NextResponse.json(
        { error: "Invalid authenticator code" },
        { status: 400 },
      );
    }

    const recoveryCodes = generateRecoveryCodes(8);
    const hashedCodes = await Promise.all(
      recoveryCodes.map((c) => bcrypt.hash(c, 10)),
    );

    await updateSettings({
      totpSecret: secret,
      totpEnabled: true,
      totpRecoveryCodes: hashedCodes,
    });

    return NextResponse.json({ success: true, recoveryCodes });
  } catch (error) {
    return NextResponse.json({ error: error.message }, { status: 500 });
  }
}
