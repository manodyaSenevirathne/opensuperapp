package plugins

import (
	// -------------------------------------------------------------------------
	// PLUGINS REGISTRY
	// Import the service implementations you want to include in the build here.
	// -------------------------------------------------------------------------

	// Default Implementations
	_ "github.com/opensuperapp/opensuperapp/backend-services/core/plugins/file-service/default-db"
	_ "github.com/opensuperapp/opensuperapp/backend-services/core/plugins/user-service/default-db"
)
