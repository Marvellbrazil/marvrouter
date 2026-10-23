import { NextResponse } from "next/server";
import { cookies } from "next/headers";
import { getSettings, updateSettings } from "@/lib/localDb";
import {
  getDashboardAuthSession,
  verifyDashboardPassword,
} from "@/lib/auth/dashboardSession";
import { verifyTotp } from "@/lib/auth/totp";

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
    const { password, totpCode } = body;
    const settings = await getSettings();

    if (!settings.totpEnabled) {
      return NextResponse.json({
        success: true,
        message: "2FA is already disabled",
      });
    }

    let isAuthorized = false;

    if (password && typeof password === "string") {
      isAuthorized = await verifyDashboardPassword(password);
    } else if (totpCode && typeof totpCode === "string") {
      isAuthorized = verifyTotp(totpCode, settings.totpSecret);
    }

    if (!isAuthorized) {
      return NextResponse.json(
        { error: "Invalid password or authenticator code" },
        { status: 401 },
      );
    }

    await updateSettings({
      totpEnabled: false,
      totpSecret: "",
      totpRecoveryCodes: [],
    });

    return NextResponse.json({ success: true });
  } catch (error) {
    return NextResponse.json({ error: error.message }, { status: 500 });
  }
}
