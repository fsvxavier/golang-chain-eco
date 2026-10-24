// POST /cart/:id/coupon { code } — reserves the coupon until payment.
export async function applyCoupon(req: Request, cartId: string): Promise<Response> {
  const { code } = await req.json();
  if (!code) return json(400, { error: "code is required" });
  const ok = await db.coupons.reserve(code, cartId); // atomic, TTL 15 min
  if (!ok) return json(409, { error: "coupon already reserved" });
  return json(200, await pricing.price(cartId));
}
