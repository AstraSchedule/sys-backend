package router

import (
	"sys-backend/config"
	"sys-backend/middleware"
	"sys-backend/router/web"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"time"
)

const routeDataTableID = "/data/:table/:id"

func Setup() *gin.Engine {
	r := gin.Default()

	origins := config.Configs.Server.Domain
	if len(origins) == 0 {
		origins = []string{"*"}
	}

	r.Use(cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "Accept", "Origin", "X-Verify-Password"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Hello World"})
	})

	api := r.Group("/web")
	{
		api.GET("/health", web.Health)

		// Auth (no auth required)
		api.POST("/auth/login", web.Login)

		// JWT protected routes
		auth := api.Group("/", middleware.JWTAuthMiddleware())
		{
			auth.GET("/auth/me", web.GetMe)
			auth.POST("/auth/change-password", web.ChangePassword)
			auth.POST("/auth/verify-password", web.VerifyPassword)

			// System users (readwrite required for write operations)
			auth.GET("/system-users", web.ListSystemUsers)
			auth.POST("/system-users", middleware.RequireWrite(), web.CreateSystemUser)
			auth.PUT("/system-users/:id", middleware.RequireWrite(), web.UpdateSystemUser)
			auth.DELETE("/system-users/:id", middleware.RequireWrite(), web.DeleteSystemUser)

			// Tenants (Cloudflare DNS CRUD)
			auth.GET("/tenants", web.ListTenants)
			auth.POST("/tenants", middleware.RequireWrite(), web.CreateTenant)
			auth.DELETE("/tenants/:id", middleware.RequireWrite(), web.DeleteTenant)

			// Astra users (tenant user management)
			auth.GET("/astra-users", web.ListAstraUsers)
			auth.POST("/astra-users", middleware.RequireWrite(), web.CreateAstraUser)
			auth.PUT("/astra-users/:id", middleware.RequireWrite(), web.UpdateAstraUser)
			auth.DELETE("/astra-users/:id", middleware.RequireWrite(), web.DeleteAstraUser)

			// Data management
			auth.GET("/data/tables", web.ListTables)
			auth.GET("/data/:table", web.ListTableData)
			auth.GET(routeDataTableID, web.GetRecord)
			auth.POST("/data/:table", middleware.RequireWrite(), web.CreateRecord)
			auth.PUT(routeDataTableID, middleware.RequireWrite(), web.UpdateRecord)
			auth.DELETE(routeDataTableID, middleware.RequireWrite(), web.DeleteRecord)

			// Backup/Restore
			auth.GET("/backup/export", web.ExportBackup)
			auth.POST("/backup/import", middleware.RequireWrite(), web.ImportBackup)

			// Database rebuild
			auth.POST("/database/rebuild", middleware.RequireWrite(), web.RebuildDatabase)
			auth.DELETE("/database/drop/:table", middleware.RequireWrite(), web.DropTable)
		}
	}

	return r
}
