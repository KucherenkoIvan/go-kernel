package ddd

import "errors"

// DomainError is an embeddable marker separating expected business errors
// from everything else. A domain error is part of a method's contract:
// transport layers map domain errors to response codes, while all other
// errors become 500 and go to error tracking.
//
//	type CouponExhaustedError struct{ ddd.DomainError }
//
//	func (e *CouponExhaustedError) Error() string { return "coupon_exhausted" }
//
// The marker method is unexported, so the only way to satisfy IsDomainError
// is to embed ddd.DomainError.
type DomainError struct{}

func (DomainError) isDomainError() {}

type domainErrorMarker interface {
	isDomainError()
}

// IsDomainError reports whether err (or any error in its chain) is an
// expected business error, i.e. a type embedding ddd.DomainError.
func IsDomainError(err error) bool {
	var m domainErrorMarker
	return errors.As(err, &m)
}
