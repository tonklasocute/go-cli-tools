// Package envcheck validates dotenv (.env) files against a schema written as
// an annotated .env.example file, and can generate such a schema from an
// existing .env.
//
// The schema is an ordinary dotenv file whose inline comments carry rules:
//
//	PORT=3000        # required,int,min=1,max=65535
//	LOG_LEVEL=info   # enum=debug|info|warn|error
//	API_KEY=         # required,secret
//	RELEASE=         # regex=^v\d+\.\d+\.\d+$
//
// Values of keys marked secret are never printed; they are replaced by
// [Mask] everywhere, including inside error messages.
package envcheck
