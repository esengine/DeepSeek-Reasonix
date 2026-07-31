# ssh_config security policy

## Supported Versions

As of September 2025, we're not aware of any security problems with ssh_config,
past or present. That said, we recommend always using the latest version of
ssh_config, and of the Go programming language, to ensure you have the most
recent security fixes.

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security vulnerability in ssh_config, please report it responsibly by following these steps:

### How to Report

Please follow the instructions outlined here to report a vulnerability
privately: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

If these are insufficient - it is not hard to find Kevin's contact information
on the Internet.

### What to Include

When reporting a vulnerability, please include a clear description of the vulnerability, steps to reproduce the issue, the potential impact, as well as any fixes you might have.

### Response Timeline

I'll try to acknowledge and patch the issue as quickly as possible.

Security advisories for this project will be published through:
- GitHub Security Advisories on this repository
- an Issue on this repository
- The project's release notes
- Go vulnerability databases

If you are using `ssh_config` and would like to be on a "pre-release"
distribution list for coordinating releases, please contact Kevin directly.

### Security Considerations

When using ssh_config, please be aware of these security considerations.

#### File System Access

This library reads SSH configuration files from the file system. Try to ensure
proper file permissions on SSH config files (typically 600 or 644), and be
cautious when parsing config files from untrusted sources.

#### Input Validation

The parser handles user-provided SSH configuration data. While we try our best
to parse the data appropriately, malformed configuration files could potentially
cause issues. Please try to validate and sanitize any configuration data from
external sources.

#### Dependencies

This project does not have any third party dependencies. Please try to keep your
Go version up to date.

## Acknowledgments

We appreciate security researchers and users who responsibly disclose vulnerabilities. Contributors who report valid security issues will be acknowledged in our security advisories (unless they prefer to remain anonymous).
