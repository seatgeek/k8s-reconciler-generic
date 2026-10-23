# Changelog

## [v1.15.1] - 2026-10-23

### Fixed

* `Reconcile` no longer reports an error when the reconcile context was canceled because the controller is shutting down (pod eviction, rollout, lost leadership) and the reconciliation failed with `context.Canceled` as a result. It returns no error and logs at V(1) instead, so these no longer show up as `Reconciler error` logs or in `controller_runtime_reconcile_errors_total`. Nothing is lost, because the next leader reconciles every object on startup. Deadlines on the reconcile context, cancellations of contexts derived inside the reconcile, and any other error during shutdown are still reported. `ReconcileComplete` still receives the original error.

## [v1.15.0] - 2026-10-01

### Changed

* Don't reconcile a subject that has a `deletionTimestamp` when `FinalizerKey()` returns `""`. Reconciliation stops before validation and returns the new `SubjectDeleting` status, so no status updates or child changes are made. Previously the subject was reconciled as if it were live, so during foreground deletion the controller recreated the children the garbage collector was deleting, and the subject was never removed. Controllers with a finalizer are unaffected.

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

