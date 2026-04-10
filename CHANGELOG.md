# Changelog

## [v1.14.1] - 2026-04-10

### Changed

* Introduce `ResourceOpts.DeleteOnPatchCalculationError` flag that will remove a resource whose update operation fails to calculate a patch.
* Revert the `DeleteOnChange` logic to previous state.

## [v1.14.0] - 2026-04-10

### Changed

* Fix `ResourceDiff.DeleteOnChange` not being respected as a result of failing `om.DefaultPatchMaker.Calculate` call.

