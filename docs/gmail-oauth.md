# Sign in with Google (optional)

App passwords work today and need nothing from us. "Sign in with Google" needs a Google OAuth client:

1. console.cloud.google.com → create a project → **APIs & Services → OAuth consent screen**: External, add the
   scope `https://mail.google.com/`, add your Google accounts as test users (or publish and go through Google's
   verification for the restricted Gmail scope).
2. **Credentials → Create credentials → OAuth client ID → Desktop app.**
3. Build with it: `GOOGLE_CLIENT_ID=… GOOGLE_CLIENT_SECRET=… app/scripts/build-go.sh` (for a desktop app the
   "secret" is not confidential, as Google documents), or run laserd with `LASER_GOOGLE_CLIENT_ID` /
   `LASER_GOOGLE_CLIENT_SECRET` set.

The flow is OAuth 2.0 for installed apps with PKCE and a one-shot loopback redirect on 127.0.0.1. Only the
`https://mail.google.com/` scope is requested (IMAP and SMTP require it). The refresh token is stored in the
Keychain.
