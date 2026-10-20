# To-do example

A small web application that shows how the gorfast pieces fit together: Sulis
password sign-in, profiles and to-dos encrypted with `crypt`, every write in
one transaction, and layered configuration. It uses plain HTML forms and no
JavaScript.

## Run it

```
cd examples/todo
cp .env.dist .env
go run github.com/borfast/gorfast/crypt/cmd/newkey
```

Paste the printed key after `TODO_CRYPT__KEYS=` in `.env`, then start it:

```
go run .
```

It prints the address it listens on. The default database is the SQLite file
`todo.db`; set `TODO_DATABASE__URL` to a `postgres://` URL to use PostgreSQL.

## Localhost only

Run it on localhost only. Anywhere else it needs TLS, and behind a proxy every
user would share one sign-in and sign-up rate limit, because the client address
would be the proxy's.

For a PostgreSQL database that is not on localhost, add `sslmode=verify-full` to
`TODO_DATABASE__URL`: with no `sslmode`, or with `require`, the driver does not
check the server's certificate.

## Email verification

Sign-in is password only, so nothing verifies an email address. Real
applications should require email verification.

## Lockout

Failure lockout is off on purpose: it lets anyone lock a person out of their
account. Turn it on only for a clear reason.

Sign-in attempts are still rate limited for each account: 10 at once, then 1
every 2 seconds. Sulis's default allows 1 every 30 seconds, slower than one IP
may try (1 every 5 seconds), so one IP could keep a person from signing in. At 1
every 2 seconds that takes several IPs, and guessing one password is still
limited to about 43,000 attempts a day.

## Busy server

Each sign-up and sign-in hashes the password with Argon2, which uses 64 MiB. At
most 4 run at once. Another one gets status 503 with `Retry-After: 1` and its
form again, with a message to try again.

## Browsers

The session cookie is `Secure`. Chrome and Firefox accept it on
`http://localhost`, but other browsers may not.

Browsers that send `Origin` without `Sec-Fetch-Site` (Safari before 16.4, Firefox
before 90) cannot submit forms, because the same-origin check refuses them.

Forms posted from a page on another port of localhost are refused too. Browsers
call that same-site, but cookies are not separated by port, so such a page could
read the CSRF cookie.

## Logs

Sulis's security events go to the log. Those from a sign-up, sign-in or sign-out
are logged when its transaction ends. Those from a transaction that rolled back
carry `transaction=rolled_back`: any change they report, such as a new account,
was undone.
