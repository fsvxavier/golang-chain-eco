// GET /coupons/:code — looks a coupon up without consuming it.
export async function getCoupon(req: Request, code: string): Promise<Response> {
  const coupon = await db.coupons.find(code);
  if (!coupon) return json(404, { error: "coupon not found" });
  if (coupon.status === "expired" || coupon.status === "consumed") {
    return json(422, { error: "coupon expired or exhausted" });
  }
  return json(200, coupon); // { code, discount_percent, status, expires_at }
}
