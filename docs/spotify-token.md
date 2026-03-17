## Instructions for fetching Spotify Refresh Token

### Step 1: Create a Spotify Application

1. Log in to the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard).
2. Click **Create an App**.
3. Provide a name and description, then click **Create**.
4. In the app details, click **Show Client Secret** and save both your **Client ID** and **Client Secret**.
5. Click **Edit Settings** and add `http://127.0.0.1:3000` as a Redirect URI.

You now have the credentials needed for Spotify API requests.

### Step 2: Authenticate using the Authorization Code Flow

1. Determine the required authorization scopes based on your needs by referring to the [Spotify API Scopes documentation](https://developer.spotify.com/documentation/web-api/concepts/scopes). Choose scopes like `user-read-currently-playing`, `user-read-recently-played` or `user-top-read` depending on the access you require.

2. Construct an authorization URL with your client ID, redirect URI, and the chosen scopes:

```
https://accounts.spotify.com/authorize?client_id=YOUR_CLIENT_ID&response_type=code&redirect_uri=http%3A%2F%2Flocalhost:3000&scope=user-read-currently-playing%20user-read-recently-played
```

Replace `YOUR_CLIENT_ID` and customize the scopes as needed.

3. After authorizing, you’ll be redirected to your specified Redirect URI (e.g., `http://127.0.0.1:3000/callback?code=YOUR_CODE`). Save the `code` parameter (`YOUR_CODE`) from the URL.

### Step 3: Exchange Authorization Code for a Refresh Token

1. Generate a Base64-encoded string of `client_id:client_secret` (use [this tool](https://www.base64encode.org/) for encoding).

2. Use the following `curl` command to request the refresh token:

```bash
curl -H "Authorization: Basic BASE64_ENCODED_CLIENT_ID:CLIENT_SECRET" \
     -d grant_type=authorization_code \
     -d code=YOUR_CODE \
     -d redirect_uri=http://localhost:3000 \
     https://accounts.spotify.com/api/token
```

3. The response will include a `refresh_token` that remains valid indefinitely unless access is revoked. Save it securely, such as in an environment variable.
