# Security rules

## Input
Validate every input server-side. Never trust a discount, price or role sent by
the client.

## Abuse
Rate-limit lookups of codes, tokens and identifiers to stop enumeration. Return
the same error for unknown and invalid codes.

## Secrets and data
Never log secrets or full card, token or coupon values. Keep personal data out
of error messages.

## Money
Reserve before charging, confirm after payment, release on failure, and expire
reservations with a TTL so a crash never holds a resource forever.
