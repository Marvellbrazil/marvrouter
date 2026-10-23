import { NextResponse } from "next/server";
import { cookies } from "next/headers";
import { getSettings } from "@/lib/localDb";
import { getDashboardAuthSession } from "@/lib/auth/dashboardSession";
import { generateTotpSecret, generateTotpUri } from "@/lib/auth/totp";

export async function POST() {
  try {
    const cookieStore = await cookies();
    const session = await getDashboardAuthSession(
      cookieStore.get("auth_token")?.value,
    );
    if (!session) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const settings = await getSettings();
    const username = settings.username || "admin";
    const secret = generateTotpSecret();
    const uri = generateTotpUri(username, secret, "9Router");

    return NextResponse.json({ secret, uri });
  } catch (error) {
    return NextResponse.json({ error: error.message }, { status: 500 });
  }
}
