package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bundebug"
)

func connect() *bun.DB {
	env := NewEnv()
	var dsn string
	if env.AppEnv == "production" {
		dsn = fmt.Sprintf("postgres://%s:%s@%s/%s", env.DbUser, env.DbPass, env.DbHost, env.DbName)
	} else {
		dsn = fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", env.DbUser, env.DbPass, env.DbHost, env.DbName)
	}
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))

	db := bun.NewDB(sqldb, pgdialect.New())
	db.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(true),
		bundebug.FromEnv("BUNDEBUG"),
	))

	return db
}

type trackerDb struct {
	db *bun.DB
}

type Item struct {
	bun.BaseModel `bun:"table:item,alias:i"`

	ID         uuid.UUID `bun:"default:gen_random_uuid()" json:"id"`
	Name       string    `json:"name"`
	Cost       float64   `json:"cost"`
	Type       string    `json:"type"`
	CategoryID uuid.UUID `bun:"type:uuid" json:"category_id"`
	UserID     int       `bun:"user_id" json:"user_id"`
}

func (trackerDb *trackerDb) addItem(c echo.Context) error {
	ctx := context.Background()

	var item *Item
	item = new(Item)
	err := c.Bind(item)
	if err != nil {
		log.Printf("Error while binding: %+v", err)
		return c.JSON(http.StatusInternalServerError, "Internal server error")
	}

	_, err = trackerDb.db.NewInsert().Model(item).Exec(ctx)
	if err != nil {
		log.Printf("Error executing insert: %v", err)
		return c.JSON(http.StatusInternalServerError, "Internal server error")
	}

	return c.JSON(http.StatusOK, "Done")
}

type GetAllItemsRow struct {
	ID         uuid.UUID        `bun:"id" json:"id"`
	Name       string           `json:"name"`
	Cost       float64          `json:"cost"`
	Type       string           `json:"type"`
	CategoryID uuid.UUID        `bun:"type:uuid" json:"category_id"`
	UserID     int              `bun:"user_id" json:"user_id"`
	CreatedAt  pgtype.Timestamp `json:"createdAt" bun:"createdAt"`
}

func (trackerDb *trackerDb) getAllItems(c echo.Context) error {
	ctx := context.Background()
	userID := c.QueryParam("user_id")

	items := []GetAllItemsRow{}
	err := trackerDb.db.NewSelect().TableExpr("item").Where("user_id = ?", userID).Scan(ctx, &items)
	if err != nil {
		log.Printf("Error while getting items: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	successData := map[string]interface{}{
		"message": "ok",
		"data":    items,
	}

	return c.JSON(http.StatusOK, successData)
}

type GetItem struct {
	ID         uuid.UUID        `json:"id" bun:"id"`
	Name       string           `json:"name" bun:"name"`
	Cost       float64          `json:"cost" bun:"cost"`
	Type       string           `json:"type" bun:"type"`
	CategoryID uuid.UUID        `json:"category_id" bun:"category_id"`
	CreatedAt  pgtype.Timestamp `json:"createdAt" bun:"createdAt"`
	UserID     int              `bun:"user_id" json:"user_id"`
}

func (trackerDb *trackerDb) getItemFromId(c echo.Context) error {
	ctx := context.Background()
	id := c.Param("id")

	var item GetItem
	err := trackerDb.db.NewSelect().TableExpr("item").Where("id = ?", id).Scan(ctx, &item)
	if err != nil {
		log.Printf("Could not fetch item: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	successData := map[string]interface{}{
		"message": "ok",
		"data":    item,
	}

	return c.JSON(http.StatusOK, successData)
}

func (trackerDb *trackerDb) deleteItem(c echo.Context) error {
	ctx := context.Background()
	id := c.Param("id")

	res, err := trackerDb.db.NewDelete().TableExpr("item").Where("id = ?", id).Exec(ctx)
	if err != nil {
		log.Printf("Error while deleting: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	successData := map[string]interface{}{
		"message": "ok",
		"data":    res,
	}

	return c.JSON(http.StatusOK, successData)
}

func (trackerDb *trackerDb) updateItem(c echo.Context) error {
	ctx := context.Background()
	value := make(map[string]interface{})

	err := c.Bind(&value)
	if err != nil {
		log.Printf("Error while binding: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	res, err := trackerDb.db.NewUpdate().Model(&value).Where("id = ?", value["id"]).TableExpr("item").Exec(ctx)
	if err != nil {
		log.Printf("Error while updating: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	successData := map[string]interface{}{
		"message": "ok",
		"data":    res,
	}

	return c.JSON(http.StatusOK, successData)
}

type CategoriesVsExpensesRow struct {
	Category string  `json:"category"`
	Expenses float64 `json:"expenses"`
	Income   float64 `json:"income"`
}

type IncomeVsExpenses struct {
	Expenses float64 `json:"expenses"`
	Income   float64 `json:"income"`
}

type MonthlyExpensesRow struct {
	Month    string  `json:"month"`
	Year     string  `json:"year"`
	Expenses float64 `json:"expenses"`
	Income   float64 `json:"income"`
}

func (trackerDb *trackerDb) getDashboardData(c echo.Context) error {
	ctx := context.Background()
	userID := c.QueryParam("user_id")

	categories := []CategoriesVsExpensesRow{}
	err := trackerDb.db.NewSelect().
		With("expense_data",
			trackerDb.db.NewSelect().
				ColumnExpr("c.name as category").
				ColumnExpr("SUM(CASE WHEN i.type = 'debit' THEN i.cost ELSE 0 END) AS expenses").
				ColumnExpr("SUM(CASE WHEN i.type = 'credit' THEN i.cost ELSE 0 END) AS income").
				TableExpr("item i").
				Join("JOIN category c ON i.category_id = c.id").
				Where("user_id = ?", userID).
				Group("c.name"),
		).
		TableExpr("expense_data").
		Scan(ctx, &categories)
	if err != nil {
		log.Printf("Error while getting categories data: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	incomeVsExpenses := IncomeVsExpenses{}
	err = trackerDb.db.NewSelect().
		ColumnExpr("SUM(CASE WHEN type = 'debit' THEN cost ELSE 0 END) AS expenses").
		ColumnExpr("SUM(CASE WHEN type = 'credit' THEN cost ELSE 0 END) AS income").
		TableExpr("item AS i").
		Where("user_id = ?", userID).
		Scan(ctx, &incomeVsExpenses)
	if err != nil {
		log.Printf("Error while getting income v/s expenses data: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	monthly := []MonthlyExpensesRow{}
	err = trackerDb.db.NewSelect().
		ColumnExpr("TO_CHAR(\"createdAt\", 'MM') AS month").
		ColumnExpr("TO_CHAR(\"createdAt\", 'YYYY') AS year").
		ColumnExpr("sum(case when i.\"type\" = 'debit' then i.\"cost\" else 0 end) as expenses").
		ColumnExpr("sum(case when i.\"type\" = 'credit' then i.\"cost\" else 0 end) as income").
		TableExpr("item AS i").
		Where("user_id = ?", userID).
		Group("month").
		Group("year").
		Order("month").
		Scan(ctx, &monthly)
	if err != nil {
		log.Printf("Error while getting monthly data: %+v", err)
		return c.JSON(http.StatusInternalServerError, err)
	}

	successData := map[string]interface{}{
		"message": "ok",
		"data": map[string]interface{}{
			"categories":       categories,
			"incomeVsExpenses": incomeVsExpenses,
			"monthly":          monthly,
		},
	}

	return c.JSON(http.StatusOK, successData)
}

func main() {
	db := connect()
	e := echo.New()
	e.Use(middleware.CORS())

	e.GET("/hello", func(c echo.Context) error {
		return c.String(http.StatusOK, "Welcome")
	})

	trackerDb := &trackerDb{
		db: db,
	}

	apiv1 := e.Group("/api/v1")
	apiv1.GET("/hello", func(c echo.Context) error {
		return c.String(http.StatusOK, "Welcome")
	})
	apiv1.POST("/item", trackerDb.addItem)
	apiv1.GET("/items", trackerDb.getAllItems)
	apiv1.GET("/items/:id", trackerDb.getItemFromId)
	apiv1.GET("/dashboard-data", trackerDb.getDashboardData)
	apiv1.DELETE("/items/:id", trackerDb.deleteItem)
	apiv1.PATCH("/update/item", trackerDb.updateItem)

	e.Logger.Fatal(e.Start(":1323"))
}
