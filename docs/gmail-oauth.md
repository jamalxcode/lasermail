# Sign in with Google (one-time setup by the owner)

With this set up, adding Gmail is: **Gmail → Sign in with Google… → pick the account in the browser → Allow.**
No app password, nothing to type. It needs a Google OAuth client, created once (about 10 minutes):

1. Open **console.cloud.google.com**, sign in, and create a project (e.g. "Laser Email").
2. **APIs & Services → Library**: search for **Gmail API** and click **Enable**.
3. **APIs & Services → OAuth consent screen** (Google Auth Platform): app name *Laser Email*, your support
   email, audience **External**. Under **Data access**, add the scope `https://mail.google.com/` (plus `openid`
   and `.../auth/userinfo.email`).
4. **Audience → Publishing status: In production** (click *Publish app*). In *Testing*, Google signs users out
   every 7 days. An unverified app in production shows a "Google hasn't verified this app" page at sign-in:
   click **Advanced → Go to Laser Email (unsafe)** to continue. That's fine for personal use (Google allows up to
   100 users); removing the page needs Google's verification for the Gmail scope, a longer process.
5. **Clients → Create client → Application type: Desktop app**, name *Laser Email*. Copy the **Client ID** and
   **Client secret**.
6. In GitHub: **jamalxcode/lasermail → Settings → Secrets and variables → Actions → New repository secret**:
   `GOOGLE_CLIENT_ID` = the client ID, and another, `GOOGLE_CLIENT_SECRET` = the client secret.

The next release then includes Sign in with Google. (For a desktop app the "client secret" isn't confidential,
as Google documents; it ends up inside the app. Keep it in GitHub secrets anyway, so it isn't in the repo.)

How it works: OAuth 2.0 for installed apps with PKCE and a one-shot loopback redirect on 127.0.0.1. The scopes are
`https://mail.google.com/` (IMAP and SMTP require it) and `openid email` (to learn which account was chosen). The
refresh token is kept in the Keychain; the Google password never passes through Laser Email.
