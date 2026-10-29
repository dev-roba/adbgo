# Security Policy

## Supported versions

Security fixes are developed against the latest code on the default branch. Until the first stable release, no version should be assumed to receive security updates automatically.

## Reporting a vulnerability

Please do not publish exploitable details, device identifiers, logs containing personal data, or credentials in a public issue. Contact the repository maintainer privately through GitHub's security reporting feature if it is enabled; otherwise open a minimal issue asking for a private reporting channel.

Include the affected commit, operating system, Android/ADB versions, reproduction steps, and impact. Redact serial numbers, IP addresses, account details, tokens, and personal files.

## Safe-use boundaries

adbgo is intended for devices you own or are authorized to administer. It does not provide lock-screen or Factory Reset Protection bypasses. Review the target serial before changing device state. Never submit passwords, PINs, private logs, or exported device configuration in bug reports.
