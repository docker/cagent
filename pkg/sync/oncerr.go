package sync

import "sync"

func OnceErr[T any](fn func() (T, error)) func() (T, error) {
	var once sync.Once
	var result T
	var err error

	return func() (T, error) {
		once.Do(func() {
			result, err = fn()
		})
		return result, err
	}
}
