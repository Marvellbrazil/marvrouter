import { NextResponse } from "next/server";
import { getProviderConnectionById } from "@/lib/localDb";
import {
  CORS_HEADERS,
  isAuthorized,
  resolveConnectionQuota,
} from "../route.js";

export async function OPTIONS() {
  return new NextResponse(null, {
    status: 204,
    headers: CORS_HEADERS,
  });
}

export async function GET(request, { params }) {
  const authorized = await isAuthorized(request);
  if (!authorized) {
    return NextResponse.json(
      { success: false, error: "Unauthorized" },
      { status: 401, headers: CORS_HEADERS },
    );
  }

  const { uuid } = await params;
  if (!uuid) {
    return NextResponse.json(
      { success: false, error: "Missing quota ID" },
      { status: 400, headers: CORS_HEADERS },
    );
  }

  const conn = await getProviderConnectionById(uuid);
  if (!conn) {
    return NextResponse.json(
      { success: false, error: "Connection not found" },
      { status: 404, headers: CORS_HEADERS },
    );
  }

  const quota = await resolveConnectionQuota(conn);

  return NextResponse.json(
    {
      success: true,
      timestamp: new Date().toISOString(),
      quota,
    },
    { headers: CORS_HEADERS },
  );
}
