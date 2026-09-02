package validator

import (
	"errors"

	"validator_dep"
)

type inner struct {
	n int
}

type request struct {
	p *inner
	q *inner
}

var errMissing = errors.New("missing")

func (r *request) Validate() error { // want Validate:"validates p"
	if r.p == nil {
		return errMissing
	}

	return nil
}

func (r *request) validateBoth() error { // want validateBoth:"validates p, q"
	if r.p == nil || r.q == nil {
		return errors.New("missing")
	}

	return nil
}

func (r *request) validateThenAllowNil() error {
	if r.q != nil {
		return nil
	}

	if r.p == nil {
		return errMissing
	}

	return nil
}

func (r *request) validateReturningLocal() error {
	err := errMissing

	if r.p == nil {
		return err
	}

	return nil
}

func (r *request) validateThenClear() error {
	if r.p == nil {
		return errMissing
	}

	r.p = nil

	return nil
}

func (r *request) normalizeAndValidate() []error {
	if r.p == nil {
		return []error{errMissing}
	}

	return nil
}

func provenByCheckedValidate(r *request) int {
	if err := r.Validate(); err != nil {
		return 0
	}

	return r.p.n
}

func provenByValidateCheckedLater(r *request) int {
	err := r.Validate()
	if err != nil {
		return 0
	}

	return r.p.n
}

func provenOnPositiveBranch(r *request) int {
	if err := r.Validate(); err == nil {
		return r.p.n
	}

	return 0
}

func provenForEveryCheckedField(r *request) int {
	if err := r.validateBoth(); err != nil {
		return 0
	}

	return r.p.n + r.q.n
}

func notProvenForUncheckedField(r *request) int {
	if err := r.Validate(); err != nil {
		return 0
	}

	return r.q.n // want "r\\.q may be nil here"
}

func notProvenWhenErrorDiscarded(r *request) int {
	_ = r.Validate()

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenResultDropped(r *request) int {
	r.Validate()

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenErrorNeverTested(r *request) int {
	err := r.Validate()
	_ = err

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenFailureFallsThrough(r *request) int {
	n := 0

	if err := r.Validate(); err != nil {
		n = 1
	}

	return n + r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenValidatorCanReturnNilFirst(r *request) int {
	if err := r.validateThenAllowNil(); err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenGuardReturnsALocal(r *request) int {
	if err := r.validateReturningLocal(); err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenValidatorClearsTheField(r *request) int {
	if err := r.validateThenClear(); err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenForAnErrorSliceValidator(r *request) int {
	errs := r.normalizeAndValidate()
	if len(errs) > 0 {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func proofDiesOnReassignment(r *request) int {
	if err := r.Validate(); err != nil {
		return 0
	}

	r.p = nil

	return r.p.n // want "r\\.p may be nil here"
}

func proofDiesOnReassignmentBeforeTheCheck(r *request) int {
	err := r.Validate()

	r.p = nil

	if err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func provenAcrossPackages(r *validator_dep.Request) int {
	if err := r.Validate(); err != nil {
		return 0
	}

	return r.P.N
}

func notProvenAcrossPackagesForUncheckedField(r *validator_dep.Request) int {
	if err := r.Validate(); err != nil {
		return 0
	}

	return r.Q.N // want "r\\.Q may be nil here"
}

func (r *request) clear() { r.p = nil }

func (r *request) validateThenCallsMethod() error {
	if r.p == nil {
		return errMissing
	}

	r.clear()

	return nil
}

func poke(_ *request) {}

func (r *request) validateThenPassesReceiver() error {
	if r.p == nil {
		return errMissing
	}

	poke(r)

	return nil
}

func notProvenWhenValidatorCallsAMethod(r *request) int {
	if err := r.validateThenCallsMethod(); err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func notProvenWhenValidatorPassesReceiver(r *request) int {
	if err := r.validateThenPassesReceiver(); err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}

func describe(_ *request) string { return "" }

func (r *request) validateLoggingReceiver() error { // want validateLoggingReceiver:"validates p"
	if r.p == nil {
		return errors.New(describe(r))
	}

	return nil
}

func provenWhenGuardOnlyLogsReceiver(r *request) int {
	if err := r.validateLoggingReceiver(); err != nil {
		return 0
	}

	return r.p.n
}

func (i inner) negative() bool { return i.n < 0 }

func (i *inner) reset() { i.n = 0 }

func describeInt(_ int) string { return "" }

func (r *request) validateWithValueMethodInCondition() error { // want validateWithValueMethodInCondition:"validates p"
	if r.p == nil {
		return errMissing
	}

	if r.p.negative() {
		return errMissing
	}

	return nil
}

func (r *request) validateWithValueArgument() error { // want validateWithValueArgument:"validates p"
	if r.p == nil {
		return errMissing
	}

	_ = describeInt(r.p.n)

	return nil
}

func (r *request) validateWithPointerMethodOnField() error {
	if r.p == nil {
		return errMissing
	}

	r.p.reset()

	return nil
}

func provenWhenConditionCallsValueMethod(r *request) int {
	if err := r.validateWithValueMethodInCondition(); err != nil {
		return 0
	}

	return r.p.n
}

func provenWhenBodyPassesValueArgument(r *request) int {
	if err := r.validateWithValueArgument(); err != nil {
		return 0
	}

	return r.p.n
}

func notProvenWhenPointerMethodOnField(r *request) int {
	if err := r.validateWithPointerMethodOnField(); err != nil {
		return 0
	}

	return r.p.n // want "r\\.p may be nil here"
}
