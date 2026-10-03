---
id: cli
title: The CLI
summary: Logging the meshploy command in by approving it in a browser, and logging it out again.
pages: [account, cli-login]
---

## Logging in {#login}

`meshploy auth login` prints a link and a short **code**. Open the link in any browser, sign in there (with two-factor, if you use it), check the page shows the same code as your terminal, and **Approve**. The CLI is then signed in as you. Your password is never typed into the terminal, so logging in on a server over SSH is safe: open the link on your own laptop.

The CLI finds your server on its own when it runs on one of your Meshploy machines, or from its last login, and says which server it chose. Elsewhere it asks for your domain, or takes it from `--url`.

## Checking the code {#code}

Approve only when the code on the page matches the one your terminal shows. A different code means someone else's terminal asked, and approving it would let them act as you: deny it instead. A code lasts ten minutes and works once.

## What it can do {#access}

A logged-in CLI can do anything you can, with your permissions, until you log it out or it goes **90 days unused**. An agent or an automation should not use your login: give it an agent and its own token instead, so it has only the access it needs.

## CLI sessions {#sessions}

Every CLI signed in as you is listed under **CLI sessions** in your settings, with the machine's name and when it was last used. **Log out** ends one at once. `meshploy auth logout` does the same from the terminal.

On a machine that cannot open a link at all, `meshploy auth login --password` asks for your email, password and two-factor code in the terminal instead, for a session that lasts a day.

## Terms {#terms}

- **CLI login** {#login -> login}: Signing the meshploy command in by approving it in a browser, with the code its terminal shows.
- **Login code** {#code -> code}: The short code shown in both the terminal and the browser. Approve only when they match.
- **CLI session** {#session -> sessions}: A CLI signed in as you. Listed in your settings, where it can be logged out; it lapses after 90 days unused.
