# Security policy

OmaStore downloads files published by third parties and installs them into
your account, so its security matters more than its size suggests.

## Supported versions

Only the latest release receives fixes. Update with the same `install.sh`
(or your package manager) before reporting.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately through
[GitHub private vulnerability reporting](https://github.com/KitsuneSemCalda/OmaStore/security/advisories/new).

Include what you can: the affected version, the steps or a proof of concept
(for example a crafted archive or manifest), and the impact you observed.
You should get an answer within a week. Once a fix is released, the advisory
is published with credit to you, unless you prefer otherwise.

## Scope

In scope, among others:

- escaping the destination while extracting an archive (path traversal,
  symlinks, hard links) or writing outside `$HOME`;
- running any downloaded file during installation;
- installing a file whose published checksum does not match;
- shadowing system commands or overwriting files that OmaStore did not create;
- injecting keys into the generated `.desktop` files or launchers;
- the frontend reaching the network, or the daemon's socket being usable by
  other users.

Out of scope: a malicious app doing harm **after** you install and run it.
OmaStore does not sandbox apps; it installs what the author published, from
the author's GitHub releases, and checks the checksum when one is published.
Report malicious apps as a regular issue so they can be reviewed.
