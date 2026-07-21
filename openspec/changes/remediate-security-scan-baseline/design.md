## Context

The release candidate at commit `51375c8d` passed the application CI and image build but failed the repository Security Scan. `govulncheck` found reachable `GO-2026-5856` paths in Go 1.26.4 and reachable `GO-2026-5764` paths through the AWS EventStream/S3 dependency chain. The production dependency audit also found vulnerable axios and form-data versions, while two existing `xlsx` advisory exceptions had expired.

The frontend is embedded into the Go binary and released as one container image. Therefore the release gate must cover both ecosystems before that image can be promoted. Production is currently healthy on the prior image and is not changed while this remediation is incomplete.

## Goals / Non-Goals

**Goals:**

- Clear all reachable backend vulnerabilities reported by the current Security Scan using the minimum supported fixed versions.
- Clear frontend production audit failures by upgrading dependencies where a supported fixed version exists.
- Treat an unpatched or incompatible `xlsx` path as an explicit stop-and-decide point.
- Keep toolchain declarations aligned across Go module metadata, CI verification, and Docker builds.
- Preserve the existing API, S3 behavior, frontend behavior, and production topology.
- Produce repeatable local and CI evidence before promoting an immutable GHCR image.

**Non-Goals:**

- No broad dependency refresh or framework upgrade.
- No API, database, authentication, authorization, billing, storage, routing, or UX redesign.
- No silent renewal of expired security exceptions.
- No building release images on the production server.
- No production cutover before the security gate is green.

## Decisions

- **Use minimum fixed versions.** Go will move to the first supported release that fixes `GO-2026-5856`; the AWS S3/EventStream dependency chain will move to versions that fix `GO-2026-5764`; axios and form-data will move to their first compatible patched versions. Alternative considered: upgrade every dependency to latest; rejected because it expands regression risk without helping this release gate.
- **Keep version declarations synchronized.** `backend/go.mod`, the Docker Go image argument, and the workflow version assertion must describe the same Go baseline. Alternative considered: remove the assertion; rejected because drift between local module metadata and the release builder would become harder to detect.
- **Let package managers resolve transitive constraints, then inspect the exact lock diff.** Direct dependencies will be changed intentionally and generated sums/lockfiles will be accepted only after reviewing the resolved versions. Alternative considered: hand-edit lockfile entries; rejected because it can create an invalid dependency graph.
- **Keep release frontend builders on pnpm 9.** The repository stores transitive security overrides under `package.json#pnpm.overrides`, which pnpm 9 and all GitHub workflows consume. Both frontend release Dockerfiles use pnpm 9 so frozen installs apply the same graph. Alternative considered: keep `deploy/Dockerfile` on `pnpm@latest`; rejected because pnpm 11 ignores that configuration location.
- **Vendor SheetJS 0.20.3 from its authoritative distribution.** SheetJS no longer publishes current Community Edition releases to npm; its official NodeJS installation guide directs pnpm users to the versioned CDN tarball and strongly recommends vendoring for stability. The official artifact is committed at `frontend/vendor/xlsx-0.20.3.tgz`, installed through `file:`, and documented with SHA-256 `8dc73fc3b00203e72d176e85b50938627c7b086e607c682e8d3c22c02bb99fe8`. This makes dependency bytes source-commit-addressable and removes the obsolete `xlsx` exceptions. Alternative considered: retain the remote URL or extend the exception date; rejected because the URL-only lock entry had no content integrity and fixed versions 0.19.3 and 0.20.2+ exist for the two findings.
- **Promote only a commit-addressable CI image.** After all checks pass, GitHub Actions builds the `linux/amd64` image, and production only pulls and recreates the `sub2api` service. Alternative considered: build on production; rejected because it previously created resource contention and weakens provenance.

## Risks / Trade-offs

- **AWS SDK upgrade changes transitive behavior** -> Restrict to the minimum fixed S3 line, run backend tests, and smoke the S3-facing paths if configured test coverage exists.
- **Go patch release changes compiler/runtime behavior** -> Align all builders and run the complete backend test suite plus image build.
- **axios upgrade changes request handling** -> Run frontend typecheck, complete unit tests, production build, and relevant browser smoke tests.
- **The vendored `xlsx` artifact changes or fails checksum verification** -> Reject the build and re-establish the artifact from the authoritative source; if no supported fixed path remains, stop before modifying exceptions or production and request the smallest acceptable policy decision.
- **Security tools or registries are temporarily unavailable** -> Do not treat incomplete scans as passing; retry later or report the unverified boundary.
- **Production replacement fails** -> Preserve the current compose/Nginx backup and previous image ID, recreate only `sub2api`, and restore the exact prior image on failed smoke checks.

## Migration Plan

1. Confirm advisory fixed-version ranges against primary vulnerability records and inspect the repository dependency graph.
2. Update Go, AWS SDK, axios, and form-data with package-manager-generated sums/lock data.
3. Resolve the `xlsx` path or stop for explicit direction if no minimal supported fix exists.
4. Run backend tests, `govulncheck`, frontend lint/typecheck/tests/build, and the production dependency audit with the repository exception checker.
5. Review the final dependency and workflow diff for unintended version churn or contract changes.
6. Commit and push the exact source; require CI, Security Scan, and image build to pass.
7. Verify the registry tag, commit metadata, platform manifest, and digest.
8. Back up the live compose/Nginx state, record the running image ID, pull the verified image, and recreate only `sub2api`.
9. Run origin and public smoke tests plus authenticated responsive browser checks. Roll back immediately on failed health, routing, auth, or core page verification.

## Open Questions

- Resolved: SheetJS documents its CDN as the authoritative source and provides the versioned `xlsx-0.20.3.tgz` package for pnpm; the verified artifact is vendored and the existing import/API surface remains `xlsx`.
