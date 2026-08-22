// Package pagination holds the page-size rules shared by every list endpoint.
package pagination

const (
	// DefaultPage is the fallback page index for list endpoints.
	DefaultPage = 1
	// DefaultPerPage sets the standard page size when none is provided.
	DefaultPerPage = 20
	// MaxPerPage caps client supplied page sizes to protect the database.
	MaxPerPage = 100
)

// NormalisePage coerces the incoming page to a positive integer.
func NormalisePage(page int) int {
	if page > 0 {
		return page
	}
	return DefaultPage
}

// NormalisePerPage ensures the page size stays within acceptable bounds.
func NormalisePerPage(perPage int) int {
	if perPage <= 0 {
		return DefaultPerPage
	}
	if perPage > MaxPerPage {
		return MaxPerPage
	}
	return perPage
}

// Offset returns the starting row index for the given page configuration.
func Offset(page, perPage int) int {
	if page <= 1 {
		return 0
	}
	return (page - 1) * perPage
}

// TotalPages reports how many pages cover total rows at the given page size.
func TotalPages(total, perPage int) int {
	perPage = NormalisePerPage(perPage)
	if total <= 0 {
		return 0
	}
	return (total + perPage - 1) / perPage
}
