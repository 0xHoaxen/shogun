package app_test

import "github.com/0xHoaxen/shogun/pkg/netguard"

// llmNotPublic is the error the HTTP client gives for a guarded address.
func llmNotPublic() error { return netguard.ErrNotPublic }
