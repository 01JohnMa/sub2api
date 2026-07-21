# Vendored SheetJS package

`xlsx-0.20.3.tgz` is the official SheetJS Community Edition 0.20.3 NodeJS package.

- Source: `https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`
- SHA-256: `8dc73fc3b00203e72d176e85b50938627c7b086e607c682e8d3c22c02bb99fe8`
- Package name/version: `xlsx@0.20.3`
- Security baseline: fixes CVE-2023-30533 and CVE-2024-22363.

Verify the vendored artifact before updating the lockfile:

```sh
shasum -a 256 frontend/vendor/xlsx-0.20.3.tgz
```

Do not replace this file without updating the source URL, checksum, package version, security evidence, lockfile, audit output, export smoke test, and frontend validation.
