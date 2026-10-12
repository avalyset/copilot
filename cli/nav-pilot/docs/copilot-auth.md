# Copilot authentication

`copilot_auth_mode` filters the environment nav-pilot hands to cplt. It does
not select an account or enforce an exclusive credential source.

| Mode | nav-pilot's current guarantee |
| --- | --- |
| `auto` or unset | Preserve the token variables without choosing between them. |
| `env_only` | Refuse launch unless `GH_TOKEN`, `GITHUB_TOKEN` or `COPILOT_GITHUB_TOKEN` has a non-blank value. Preserve their values. |
| `gh_only` | Remove all three variables, including empty entries, before starting cplt. This is a legacy environment filter. Exclusive gh-source enforcement is unsupported. |

Both the regular Copilot launch and staged Tier 2 launch apply this filter.
OpenCode and Pi do not apply `copilot_auth_mode`; their credentials belong to
their own client and cplt launch paths.

cplt can deny exported variables through `deny.env`, inject a host gh token
into the sandbox environment, or retain access to Copilot's stored login.
On macOS, default Copilot Keychain substitution can inject a gh token even
with the gh guard off. The guard's serve-once token file is a separate
credential channel. Removing the parent variables therefore neither forces
gh authentication nor keeps tokens out of the sandbox environment.

The filter is retained for existing configurations. Changing `gh_only` to
refuse every launch would break those configurations without providing an
enforceable gh-only replacement. Use `auto` when no environment restriction
is intended. Neither restrictive mode guarantees the identity used by Copilot.

## Integration requirements

The intended order is explicit environment token, then the selected Copilot
stored login, then host gh fallback. Current nav-pilot does not implement that
resolver, and the current cplt interface does not enforce that order.

Before wiring source selection into nav-pilot, cplt needs a documented,
versioned launch contract that:

- Resolves the selected Copilot login on the host, preserving its account and
  host instead of substituting gh's identity. Any interactive login runs on
  the host through a trusted client, with its native credential storage.
- Applies the agreed environment-token precedence and explicitly reports the
  selected source and identity without exposing the token.
- Refuses a requested exclusive source when it is unavailable or cannot be
  delivered under the effective sandbox policy. It must not fall back silently.
- Keeps the sandboxed gh identity separate from Copilot's identity, including
  the gh guard's execution and token-cache channels.

Kernel-level cplt tests must cover exported tokens denied by `deny.env`,
Keychain substitution on and off, the gh guard on and off, different Copilot
and gh accounts, and missing credentials. nav-pilot's fake-cplt tests prove
only the environment handed to cplt, not the identity used inside it.

There is no Copilot SDK dependency or SDK launcher in this Go module. An SDK
consumer outside this tree needs the same host-resolution contract and an
explicit credential handoff that does not start an unsandboxed SDK-managed
CLI. SDK-specific credential options must be verified in that consumer.

`nav-pilot auth login` is separate. It stores nav-pilot's own GitHub App token
for nav-pilot services; that token is not handed to the Copilot launcher.

## First login before the sandbox

Setup, doctor, regular launches and staged launches never start login or OAuth.
gh documents automatic plaintext fallback when its credential store fails;
omitting `--insecure-storage` does not enforce secure-only storage. Automated
login remains withheld until gh provides a documented, tested secure-only
contract that fails before storing a token when the OS credential store fails.

Setup and doctor share a trusted host gh status probe, with the five
token/host/repo overrides below removed, a home-directory working directory and
a five-second deadline. Project PATH entries and gh wrappers cannot select the
probe binary. Nested cplt runs report unknown host status without probing.
The probe reads status metadata only; it neither extracts tokens nor verifies
Copilot entitlement, native login, or sandbox credential delivery.

With no non-blank exported token or github.com user in gh's `hosts.yml`, a
Copilot launch prints recovery advice before starting cplt. It does not run
login or refuse the launch: native Copilot Keychain credentials may still work.
Local BYOK sessions do not need this advice. Setup and doctor also show it when
their gh sign-in check fails and no other evidence is available.

If Copilot needs login, leave the sandbox and run in a host terminal:

```sh
(unset GH_TOKEN GITHUB_TOKEN COPILOT_GITHUB_TOKEN GH_HOST GH_REPO; gh auth login --hostname github.com --web)
(unset GH_TOKEN GITHUB_TOKEN COPILOT_GITHUB_TOKEN GH_HOST GH_REPO; gh auth status --hostname github.com)
```

Before restarting, require github.com to report `(keyring)`. If gh reports a
config-file path, stop and fix credential-store access. Do not restart using
that plaintext fallback. The status check reports storage metadata, not a
secure-only guarantee for the preceding login. Then restart:

```sh
(unset GH_TOKEN GITHUB_TOKEN COPILOT_GITHUB_TOKEN GH_HOST GH_REPO; nav-pilot)
```

Install gh first if it is missing. Choose the account with Copilot access;
this also changes gh's identity. cplt blocks browser opening by default, so
Copilot's `/login` inside the sandbox may fail. The login record is only
evidence: the launch does not validate the token over the network or verify
that cplt can deliver it. The subshells clear inherited tokens and host/repo
overrides for both login and restart without modifying your parent shell.
This matters with `gh_only`, which filters only cplt's child environment.
Both `gh_only` and `env_only` print their unsupported exclusive-source
limitations at launch. `env_only` checks a non-blank source variable, not which
credential reaches Copilot after cplt applies its policy.

OpenCode and Pi also use secure host gh login. Current cplt can supply the host
credential to sandboxed gh through the gh guard's exec-token cache without
moving it out of the OS keychain. This requires the guard to be enabled, a
trusted gh binary, and `GH_TOKEN` not denied. If push or PR authentication fails,
run `cplt doctor` and `cplt config get gh_guard.enabled` on the host. Upgrade
cplt if cache support is missing; do not switch to `--insecure-storage`.
