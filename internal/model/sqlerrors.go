package model

// FieldErrors returns every *FieldError in err's tree (wrapped with %w,
// errors.Join or a multi-error Unwrap() []error), depth first; nil if none.
func FieldErrors(err error) []*FieldError {
	var out []*FieldError
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if fe, ok := e.(*FieldError); ok {
			out = append(out, fe)
			return
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, x := range u.Unwrap() {
				walk(x)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	return out
}

// FieldErrorArgs returns errorArgs (Field, Key, Args...) of the first
// *FieldError in err with prefix put before Field ("mapping." for a
// profile's mapping, "mappings[1]." ...); nil if err carries none. The
// sqlcheck mapping / manual SQL errors name fields relative to the
// mapping, like Mapping.Validate.
func FieldErrorArgs(err error, prefix string) []string {
	fe := AsFieldError(err)
	if fe == nil {
		return nil
	}
	return append([]string{prefix + fe.Field, fe.Key}, fe.Args...)
}
