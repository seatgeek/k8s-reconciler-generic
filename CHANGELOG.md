# Changelog

## [v1.14.2] - 2026-10-01

### Fixed

* `WithoutFinalizationMixin.Finalize` now returns `(FinalizationAction, error)`, matching `Logic.Finalize`. Previously the mixin didn't satisfy `Logic`, so embedding it didn't provide a usable `Finalize`.

### Changed

* `FinalizerKey` and `Finalize` moved into a new `Finalizer` interface, which `Logic` embeds. Existing `Logic` implementations are unaffected. `WithoutFinalizationMixin` is checked against `Finalizer` at compile time.

## [v1.14.1] - 2026-04-10

### Changed

* Introduce `ResourceOpts.DeleteOnPatchCalculationError` flag that will remove a resource whose update operation fails to calculate a patch.
* Revert the `DeleteOnChange` logic to previous state.

## [v1.14.0] - 2026-04-10

### Changed

* Fix `ResourceDiff.DeleteOnChange` not being respected as a result of failing `om.DefaultPatchMaker.Calculate` call.

