# Security rules

## Rendering
Render user and API text as text. Never inject it as raw HTML
(dangerouslySetInnerHTML, innerHTML); if HTML is unavoidable, sanitize it with a
vetted library. Never build a URL, a style or a script from unvalidated input;
allow only http and https in links you build.

## Secrets and client code
The browser is public. Put no secret, API key, token or private URL in client
code, in the bundle, in an environment variable that is shipped to the browser,
or in a log. Keep tokens out of local storage when a secure, http-only cookie
can carry them. Do not log full tokens, coupon codes or personal data.

## Input and trust
Validate input on the client for the user and treat the server as the authority.
Never trust a discount, a price or a role computed in the client. Handle a
rejected request with a clear message that does not reveal internals.

## Requests
Send the CSRF token the API requires, use the credentials mode the API expects,
and call only the origins the application needs. Do not retry a request that is
not safe to repeat; disable the control while it is loading so a double submit
sends one request.

## Dependencies
Add a dependency only when it earns its weight, pin versions, and read what a
new package does before installing it.
