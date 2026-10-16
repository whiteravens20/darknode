Put two files here, each holding one secret and nothing else:

- `db_password` — the database password.
- `server_secret` — keys the HMACs of recovery and invite codes. Keep it and
  back it up with the database: change it and every code already handed out
  stops working.

Make them with:

    openssl rand -base64 32 > db_password
    openssl rand -base64 48 > server_secret
    chmod 600 db_password server_secret

Neither file belongs in version control; the repository ignores both.
