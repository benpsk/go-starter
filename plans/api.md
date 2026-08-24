## Response Format
Use one consistent JSON response shape for every API response:

All JSON field names must use `snake_case`. This applies to the response
envelope, resource fields, nested objects, pagination metadata, and validation
error keys. Do not rely on Go's default struct-field encoding; every exported
response field must have an explicit `json:"snake_case_name"` tag.

```json
{
  "data": null,
  "error": null,
  "errors": null
}
```

Fields:

- `data`: success payload, or `null` on failure.
- `error`: object with a machine-readable `code` and a user-readable `message`, or `null` on success.
- `errors`: field validation errors keyed by request field name (each value an array of messages), or `null` when not a validation error.

Success with data:

```json
{
  "data": {
    "id": 1,
    "name": "Example"
  },
  "error": null,
  "errors": null
}
```

Success without resource data must still include a message:

```json
{
  "data": {
    "message": "Operation completed successfully"
  },
  "error": null,
  "errors": null
}
```

General error:

```json
{
  "data": null,
  "error": {
    "code": "server_error",
    "message": "Something went wrong"
  },
  "errors": null
}
```

Validation error:

```json
{
  "data": null,
  "error": {
    "code": "validation_error",
    "message": "Please check the form fields"
  },
  "errors": {
    "email": ["Email is required"],
    "password": ["Password must be at least 8 characters"]
  }
}
```

Paginated success:

```json
{
  "data": {
    "items": [
      {
        "id": 1,
        "name": "Example"
      }
    ],
    "pagination": {
      "total": 100,
      "page": 1,
      "per_page": 20,
      "last_page": 5
    }
  },
  "error": null,
  "errors": null
}
```

Pagination rules:
- `page` starts at `1`.
- `per_page` defaults to `50`.
- `per_page` max is `100`.
- `last_page` is always at least `1`, even when `total` is `0`.
- Invalid `page` or `per_page` returns `422`.
