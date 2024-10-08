// Code generated by SQLBoiler 4.16.2 (https://github.com/volatiletech/sqlboiler). DO NOT EDIT.
// This file is meant to be re-generated in place and/or deleted at any time.

package models

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/friendsofgo/errors"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"github.com/volatiletech/sqlboiler/v4/queries/qmhelper"
	"github.com/volatiletech/strmangle"
)

// Deployer is an object representing the database table.
type Deployer struct {
	OwnerAddress    string `boil:"owner_address" json:"owner_address" toml:"owner_address" yaml:"owner_address"`
	DeployerAddress string `boil:"deployer_address" json:"deployer_address" toml:"deployer_address" yaml:"deployer_address"`
	GnosisSafe      bool   `boil:"gnosis_safe" json:"gnosis_safe" toml:"gnosis_safe" yaml:"gnosis_safe"`
	TXHash          string `boil:"tx_hash" json:"tx_hash" toml:"tx_hash" yaml:"tx_hash"`

	R *deployerR `boil:"-" json:"-" toml:"-" yaml:"-"`
	L deployerL  `boil:"-" json:"-" toml:"-" yaml:"-"`
}

var DeployerColumns = struct {
	OwnerAddress    string
	DeployerAddress string
	GnosisSafe      string
	TXHash          string
}{
	OwnerAddress:    "owner_address",
	DeployerAddress: "deployer_address",
	GnosisSafe:      "gnosis_safe",
	TXHash:          "tx_hash",
}

var DeployerTableColumns = struct {
	OwnerAddress    string
	DeployerAddress string
	GnosisSafe      string
	TXHash          string
}{
	OwnerAddress:    "deployers.owner_address",
	DeployerAddress: "deployers.deployer_address",
	GnosisSafe:      "deployers.gnosis_safe",
	TXHash:          "deployers.tx_hash",
}

// Generated where

type whereHelperbool struct{ field string }

func (w whereHelperbool) EQ(x bool) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.EQ, x) }
func (w whereHelperbool) NEQ(x bool) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.NEQ, x) }
func (w whereHelperbool) LT(x bool) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.LT, x) }
func (w whereHelperbool) LTE(x bool) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.LTE, x) }
func (w whereHelperbool) GT(x bool) qm.QueryMod  { return qmhelper.Where(w.field, qmhelper.GT, x) }
func (w whereHelperbool) GTE(x bool) qm.QueryMod { return qmhelper.Where(w.field, qmhelper.GTE, x) }

var DeployerWhere = struct {
	OwnerAddress    whereHelperstring
	DeployerAddress whereHelperstring
	GnosisSafe      whereHelperbool
	TXHash          whereHelperstring
}{
	OwnerAddress:    whereHelperstring{field: "\"deployers\".\"owner_address\""},
	DeployerAddress: whereHelperstring{field: "\"deployers\".\"deployer_address\""},
	GnosisSafe:      whereHelperbool{field: "\"deployers\".\"gnosis_safe\""},
	TXHash:          whereHelperstring{field: "\"deployers\".\"tx_hash\""},
}

// DeployerRels is where relationship names are stored.
var DeployerRels = struct {
}{}

// deployerR is where relationships are stored.
type deployerR struct {
}

// NewStruct creates a new relationship struct
func (*deployerR) NewStruct() *deployerR {
	return &deployerR{}
}

// deployerL is where Load methods for each relationship are stored.
type deployerL struct{}

var (
	deployerAllColumns            = []string{"owner_address", "deployer_address", "gnosis_safe", "tx_hash"}
	deployerColumnsWithoutDefault = []string{"owner_address", "deployer_address", "gnosis_safe", "tx_hash"}
	deployerColumnsWithDefault    = []string{}
	deployerPrimaryKeyColumns     = []string{"owner_address"}
	deployerGeneratedColumns      = []string{}
)

type (
	// DeployerSlice is an alias for a slice of pointers to Deployer.
	// This should almost always be used instead of []Deployer.
	DeployerSlice []*Deployer
	// DeployerHook is the signature for custom Deployer hook methods
	DeployerHook func(context.Context, boil.ContextExecutor, *Deployer) error

	deployerQuery struct {
		*queries.Query
	}
)

// Cache for insert, update and upsert
var (
	deployerType                 = reflect.TypeOf(&Deployer{})
	deployerMapping              = queries.MakeStructMapping(deployerType)
	deployerPrimaryKeyMapping, _ = queries.BindMapping(deployerType, deployerMapping, deployerPrimaryKeyColumns)
	deployerInsertCacheMut       sync.RWMutex
	deployerInsertCache          = make(map[string]insertCache)
	deployerUpdateCacheMut       sync.RWMutex
	deployerUpdateCache          = make(map[string]updateCache)
	deployerUpsertCacheMut       sync.RWMutex
	deployerUpsertCache          = make(map[string]insertCache)
)

var (
	// Force time package dependency for automated UpdatedAt/CreatedAt.
	_ = time.Second
	// Force qmhelper dependency for where clause generation (which doesn't
	// always happen)
	_ = qmhelper.Where
)

var deployerAfterSelectMu sync.Mutex
var deployerAfterSelectHooks []DeployerHook

var deployerBeforeInsertMu sync.Mutex
var deployerBeforeInsertHooks []DeployerHook
var deployerAfterInsertMu sync.Mutex
var deployerAfterInsertHooks []DeployerHook

var deployerBeforeUpdateMu sync.Mutex
var deployerBeforeUpdateHooks []DeployerHook
var deployerAfterUpdateMu sync.Mutex
var deployerAfterUpdateHooks []DeployerHook

var deployerBeforeDeleteMu sync.Mutex
var deployerBeforeDeleteHooks []DeployerHook
var deployerAfterDeleteMu sync.Mutex
var deployerAfterDeleteHooks []DeployerHook

var deployerBeforeUpsertMu sync.Mutex
var deployerBeforeUpsertHooks []DeployerHook
var deployerAfterUpsertMu sync.Mutex
var deployerAfterUpsertHooks []DeployerHook

// doAfterSelectHooks executes all "after Select" hooks.
func (o *Deployer) doAfterSelectHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerAfterSelectHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeInsertHooks executes all "before insert" hooks.
func (o *Deployer) doBeforeInsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerBeforeInsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterInsertHooks executes all "after Insert" hooks.
func (o *Deployer) doAfterInsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerAfterInsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeUpdateHooks executes all "before Update" hooks.
func (o *Deployer) doBeforeUpdateHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerBeforeUpdateHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterUpdateHooks executes all "after Update" hooks.
func (o *Deployer) doAfterUpdateHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerAfterUpdateHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeDeleteHooks executes all "before Delete" hooks.
func (o *Deployer) doBeforeDeleteHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerBeforeDeleteHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterDeleteHooks executes all "after Delete" hooks.
func (o *Deployer) doAfterDeleteHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerAfterDeleteHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeUpsertHooks executes all "before Upsert" hooks.
func (o *Deployer) doBeforeUpsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerBeforeUpsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterUpsertHooks executes all "after Upsert" hooks.
func (o *Deployer) doAfterUpsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range deployerAfterUpsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// AddDeployerHook registers your hook function for all future operations.
func AddDeployerHook(hookPoint boil.HookPoint, deployerHook DeployerHook) {
	switch hookPoint {
	case boil.AfterSelectHook:
		deployerAfterSelectMu.Lock()
		deployerAfterSelectHooks = append(deployerAfterSelectHooks, deployerHook)
		deployerAfterSelectMu.Unlock()
	case boil.BeforeInsertHook:
		deployerBeforeInsertMu.Lock()
		deployerBeforeInsertHooks = append(deployerBeforeInsertHooks, deployerHook)
		deployerBeforeInsertMu.Unlock()
	case boil.AfterInsertHook:
		deployerAfterInsertMu.Lock()
		deployerAfterInsertHooks = append(deployerAfterInsertHooks, deployerHook)
		deployerAfterInsertMu.Unlock()
	case boil.BeforeUpdateHook:
		deployerBeforeUpdateMu.Lock()
		deployerBeforeUpdateHooks = append(deployerBeforeUpdateHooks, deployerHook)
		deployerBeforeUpdateMu.Unlock()
	case boil.AfterUpdateHook:
		deployerAfterUpdateMu.Lock()
		deployerAfterUpdateHooks = append(deployerAfterUpdateHooks, deployerHook)
		deployerAfterUpdateMu.Unlock()
	case boil.BeforeDeleteHook:
		deployerBeforeDeleteMu.Lock()
		deployerBeforeDeleteHooks = append(deployerBeforeDeleteHooks, deployerHook)
		deployerBeforeDeleteMu.Unlock()
	case boil.AfterDeleteHook:
		deployerAfterDeleteMu.Lock()
		deployerAfterDeleteHooks = append(deployerAfterDeleteHooks, deployerHook)
		deployerAfterDeleteMu.Unlock()
	case boil.BeforeUpsertHook:
		deployerBeforeUpsertMu.Lock()
		deployerBeforeUpsertHooks = append(deployerBeforeUpsertHooks, deployerHook)
		deployerBeforeUpsertMu.Unlock()
	case boil.AfterUpsertHook:
		deployerAfterUpsertMu.Lock()
		deployerAfterUpsertHooks = append(deployerAfterUpsertHooks, deployerHook)
		deployerAfterUpsertMu.Unlock()
	}
}

// One returns a single deployer record from the query.
func (q deployerQuery) One(ctx context.Context, exec boil.ContextExecutor) (*Deployer, error) {
	o := &Deployer{}

	queries.SetLimit(q.Query, 1)

	err := q.Bind(ctx, exec, o)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "models: failed to execute a one query for deployers")
	}

	if err := o.doAfterSelectHooks(ctx, exec); err != nil {
		return o, err
	}

	return o, nil
}

// All returns all Deployer records from the query.
func (q deployerQuery) All(ctx context.Context, exec boil.ContextExecutor) (DeployerSlice, error) {
	var o []*Deployer

	err := q.Bind(ctx, exec, &o)
	if err != nil {
		return nil, errors.Wrap(err, "models: failed to assign all query results to Deployer slice")
	}

	if len(deployerAfterSelectHooks) != 0 {
		for _, obj := range o {
			if err := obj.doAfterSelectHooks(ctx, exec); err != nil {
				return o, err
			}
		}
	}

	return o, nil
}

// Count returns the count of all Deployer records in the query.
func (q deployerQuery) Count(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	var count int64

	queries.SetSelect(q.Query, nil)
	queries.SetCount(q.Query)

	err := q.Query.QueryRowContext(ctx, exec).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to count deployers rows")
	}

	return count, nil
}

// Exists checks if the row exists in the table.
func (q deployerQuery) Exists(ctx context.Context, exec boil.ContextExecutor) (bool, error) {
	var count int64

	queries.SetSelect(q.Query, nil)
	queries.SetCount(q.Query)
	queries.SetLimit(q.Query, 1)

	err := q.Query.QueryRowContext(ctx, exec).Scan(&count)
	if err != nil {
		return false, errors.Wrap(err, "models: failed to check if deployers exists")
	}

	return count > 0, nil
}

// Deployers retrieves all the records using an executor.
func Deployers(mods ...qm.QueryMod) deployerQuery {
	mods = append(mods, qm.From("\"deployers\""))
	q := NewQuery(mods...)
	if len(queries.GetSelect(q)) == 0 {
		queries.SetSelect(q, []string{"\"deployers\".*"})
	}

	return deployerQuery{q}
}

// FindDeployer retrieves a single record by ID with an executor.
// If selectCols is empty Find will return all columns.
func FindDeployer(ctx context.Context, exec boil.ContextExecutor, ownerAddress string, selectCols ...string) (*Deployer, error) {
	deployerObj := &Deployer{}

	sel := "*"
	if len(selectCols) > 0 {
		sel = strings.Join(strmangle.IdentQuoteSlice(dialect.LQ, dialect.RQ, selectCols), ",")
	}
	query := fmt.Sprintf(
		"select %s from \"deployers\" where \"owner_address\"=$1", sel,
	)

	q := queries.Raw(query, ownerAddress)

	err := q.Bind(ctx, exec, deployerObj)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "models: unable to select from deployers")
	}

	if err = deployerObj.doAfterSelectHooks(ctx, exec); err != nil {
		return deployerObj, err
	}

	return deployerObj, nil
}

// Insert a single record using an executor.
// See boil.Columns.InsertColumnSet documentation to understand column list inference for inserts.
func (o *Deployer) Insert(ctx context.Context, exec boil.ContextExecutor, columns boil.Columns) error {
	if o == nil {
		return errors.New("models: no deployers provided for insertion")
	}

	var err error

	if err := o.doBeforeInsertHooks(ctx, exec); err != nil {
		return err
	}

	nzDefaults := queries.NonZeroDefaultSet(deployerColumnsWithDefault, o)

	key := makeCacheKey(columns, nzDefaults)
	deployerInsertCacheMut.RLock()
	cache, cached := deployerInsertCache[key]
	deployerInsertCacheMut.RUnlock()

	if !cached {
		wl, returnColumns := columns.InsertColumnSet(
			deployerAllColumns,
			deployerColumnsWithDefault,
			deployerColumnsWithoutDefault,
			nzDefaults,
		)

		cache.valueMapping, err = queries.BindMapping(deployerType, deployerMapping, wl)
		if err != nil {
			return err
		}
		cache.retMapping, err = queries.BindMapping(deployerType, deployerMapping, returnColumns)
		if err != nil {
			return err
		}
		if len(wl) != 0 {
			cache.query = fmt.Sprintf("INSERT INTO \"deployers\" (\"%s\") %%sVALUES (%s)%%s", strings.Join(wl, "\",\""), strmangle.Placeholders(dialect.UseIndexPlaceholders, len(wl), 1, 1))
		} else {
			cache.query = "INSERT INTO \"deployers\" %sDEFAULT VALUES%s"
		}

		var queryOutput, queryReturning string

		if len(cache.retMapping) != 0 {
			queryReturning = fmt.Sprintf(" RETURNING \"%s\"", strings.Join(returnColumns, "\",\""))
		}

		cache.query = fmt.Sprintf(cache.query, queryOutput, queryReturning)
	}

	value := reflect.Indirect(reflect.ValueOf(o))
	vals := queries.ValuesFromMapping(value, cache.valueMapping)

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, cache.query)
		fmt.Fprintln(writer, vals)
	}

	if len(cache.retMapping) != 0 {
		err = exec.QueryRowContext(ctx, cache.query, vals...).Scan(queries.PtrsFromMapping(value, cache.retMapping)...)
	} else {
		_, err = exec.ExecContext(ctx, cache.query, vals...)
	}

	if err != nil {
		return errors.Wrap(err, "models: unable to insert into deployers")
	}

	if !cached {
		deployerInsertCacheMut.Lock()
		deployerInsertCache[key] = cache
		deployerInsertCacheMut.Unlock()
	}

	return o.doAfterInsertHooks(ctx, exec)
}

// Update uses an executor to update the Deployer.
// See boil.Columns.UpdateColumnSet documentation to understand column list inference for updates.
// Update does not automatically update the record in case of default values. Use .Reload() to refresh the records.
func (o *Deployer) Update(ctx context.Context, exec boil.ContextExecutor, columns boil.Columns) (int64, error) {
	var err error
	if err = o.doBeforeUpdateHooks(ctx, exec); err != nil {
		return 0, err
	}
	key := makeCacheKey(columns, nil)
	deployerUpdateCacheMut.RLock()
	cache, cached := deployerUpdateCache[key]
	deployerUpdateCacheMut.RUnlock()

	if !cached {
		wl := columns.UpdateColumnSet(
			deployerAllColumns,
			deployerPrimaryKeyColumns,
		)

		if !columns.IsWhitelist() {
			wl = strmangle.SetComplement(wl, []string{"created_at"})
		}
		if len(wl) == 0 {
			return 0, errors.New("models: unable to update deployers, could not build whitelist")
		}

		cache.query = fmt.Sprintf("UPDATE \"deployers\" SET %s WHERE %s",
			strmangle.SetParamNames("\"", "\"", 1, wl),
			strmangle.WhereClause("\"", "\"", len(wl)+1, deployerPrimaryKeyColumns),
		)
		cache.valueMapping, err = queries.BindMapping(deployerType, deployerMapping, append(wl, deployerPrimaryKeyColumns...))
		if err != nil {
			return 0, err
		}
	}

	values := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(o)), cache.valueMapping)

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, cache.query)
		fmt.Fprintln(writer, values)
	}
	var result sql.Result
	result, err = exec.ExecContext(ctx, cache.query, values...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update deployers row")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by update for deployers")
	}

	if !cached {
		deployerUpdateCacheMut.Lock()
		deployerUpdateCache[key] = cache
		deployerUpdateCacheMut.Unlock()
	}

	return rowsAff, o.doAfterUpdateHooks(ctx, exec)
}

// UpdateAll updates all rows with the specified column values.
func (q deployerQuery) UpdateAll(ctx context.Context, exec boil.ContextExecutor, cols M) (int64, error) {
	queries.SetUpdate(q.Query, cols)

	result, err := q.Query.ExecContext(ctx, exec)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update all for deployers")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to retrieve rows affected for deployers")
	}

	return rowsAff, nil
}

// UpdateAll updates all rows with the specified column values, using an executor.
func (o DeployerSlice) UpdateAll(ctx context.Context, exec boil.ContextExecutor, cols M) (int64, error) {
	ln := int64(len(o))
	if ln == 0 {
		return 0, nil
	}

	if len(cols) == 0 {
		return 0, errors.New("models: update all requires at least one column argument")
	}

	colNames := make([]string, len(cols))
	args := make([]interface{}, len(cols))

	i := 0
	for name, value := range cols {
		colNames[i] = name
		args[i] = value
		i++
	}

	// Append all of the primary key values for each column
	for _, obj := range o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), deployerPrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := fmt.Sprintf("UPDATE \"deployers\" SET %s WHERE %s",
		strmangle.SetParamNames("\"", "\"", 1, colNames),
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), len(colNames)+1, deployerPrimaryKeyColumns, len(o)))

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args...)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update all in deployer slice")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to retrieve rows affected all in update all deployer")
	}
	return rowsAff, nil
}

// Upsert attempts an insert using an executor, and does an update or ignore on conflict.
// See boil.Columns documentation for how to properly use updateColumns and insertColumns.
func (o *Deployer) Upsert(ctx context.Context, exec boil.ContextExecutor, updateOnConflict bool, conflictColumns []string, updateColumns, insertColumns boil.Columns, opts ...UpsertOptionFunc) error {
	if o == nil {
		return errors.New("models: no deployers provided for upsert")
	}

	if err := o.doBeforeUpsertHooks(ctx, exec); err != nil {
		return err
	}

	nzDefaults := queries.NonZeroDefaultSet(deployerColumnsWithDefault, o)

	// Build cache key in-line uglily - mysql vs psql problems
	buf := strmangle.GetBuffer()
	if updateOnConflict {
		buf.WriteByte('t')
	} else {
		buf.WriteByte('f')
	}
	buf.WriteByte('.')
	for _, c := range conflictColumns {
		buf.WriteString(c)
	}
	buf.WriteByte('.')
	buf.WriteString(strconv.Itoa(updateColumns.Kind))
	for _, c := range updateColumns.Cols {
		buf.WriteString(c)
	}
	buf.WriteByte('.')
	buf.WriteString(strconv.Itoa(insertColumns.Kind))
	for _, c := range insertColumns.Cols {
		buf.WriteString(c)
	}
	buf.WriteByte('.')
	for _, c := range nzDefaults {
		buf.WriteString(c)
	}
	key := buf.String()
	strmangle.PutBuffer(buf)

	deployerUpsertCacheMut.RLock()
	cache, cached := deployerUpsertCache[key]
	deployerUpsertCacheMut.RUnlock()

	var err error

	if !cached {
		insert, _ := insertColumns.InsertColumnSet(
			deployerAllColumns,
			deployerColumnsWithDefault,
			deployerColumnsWithoutDefault,
			nzDefaults,
		)

		update := updateColumns.UpdateColumnSet(
			deployerAllColumns,
			deployerPrimaryKeyColumns,
		)

		if updateOnConflict && len(update) == 0 {
			return errors.New("models: unable to upsert deployers, could not build update column list")
		}

		ret := strmangle.SetComplement(deployerAllColumns, strmangle.SetIntersect(insert, update))

		conflict := conflictColumns
		if len(conflict) == 0 && updateOnConflict && len(update) != 0 {
			if len(deployerPrimaryKeyColumns) == 0 {
				return errors.New("models: unable to upsert deployers, could not build conflict column list")
			}

			conflict = make([]string, len(deployerPrimaryKeyColumns))
			copy(conflict, deployerPrimaryKeyColumns)
		}
		cache.query = buildUpsertQueryPostgres(dialect, "\"deployers\"", updateOnConflict, ret, update, conflict, insert, opts...)

		cache.valueMapping, err = queries.BindMapping(deployerType, deployerMapping, insert)
		if err != nil {
			return err
		}
		if len(ret) != 0 {
			cache.retMapping, err = queries.BindMapping(deployerType, deployerMapping, ret)
			if err != nil {
				return err
			}
		}
	}

	value := reflect.Indirect(reflect.ValueOf(o))
	vals := queries.ValuesFromMapping(value, cache.valueMapping)
	var returns []interface{}
	if len(cache.retMapping) != 0 {
		returns = queries.PtrsFromMapping(value, cache.retMapping)
	}

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, cache.query)
		fmt.Fprintln(writer, vals)
	}
	if len(cache.retMapping) != 0 {
		err = exec.QueryRowContext(ctx, cache.query, vals...).Scan(returns...)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil // Postgres doesn't return anything when there's no update
		}
	} else {
		_, err = exec.ExecContext(ctx, cache.query, vals...)
	}
	if err != nil {
		return errors.Wrap(err, "models: unable to upsert deployers")
	}

	if !cached {
		deployerUpsertCacheMut.Lock()
		deployerUpsertCache[key] = cache
		deployerUpsertCacheMut.Unlock()
	}

	return o.doAfterUpsertHooks(ctx, exec)
}

// Delete deletes a single Deployer record with an executor.
// Delete will match against the primary key column to find the record to delete.
func (o *Deployer) Delete(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if o == nil {
		return 0, errors.New("models: no Deployer provided for delete")
	}

	if err := o.doBeforeDeleteHooks(ctx, exec); err != nil {
		return 0, err
	}

	args := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(o)), deployerPrimaryKeyMapping)
	sql := "DELETE FROM \"deployers\" WHERE \"owner_address\"=$1"

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args...)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete from deployers")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by delete for deployers")
	}

	if err := o.doAfterDeleteHooks(ctx, exec); err != nil {
		return 0, err
	}

	return rowsAff, nil
}

// DeleteAll deletes all matching rows.
func (q deployerQuery) DeleteAll(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if q.Query == nil {
		return 0, errors.New("models: no deployerQuery provided for delete all")
	}

	queries.SetDelete(q.Query)

	result, err := q.Query.ExecContext(ctx, exec)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete all from deployers")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by deleteall for deployers")
	}

	return rowsAff, nil
}

// DeleteAll deletes all rows in the slice, using an executor.
func (o DeployerSlice) DeleteAll(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if len(o) == 0 {
		return 0, nil
	}

	if len(deployerBeforeDeleteHooks) != 0 {
		for _, obj := range o {
			if err := obj.doBeforeDeleteHooks(ctx, exec); err != nil {
				return 0, err
			}
		}
	}

	var args []interface{}
	for _, obj := range o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), deployerPrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := "DELETE FROM \"deployers\" WHERE " +
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), 1, deployerPrimaryKeyColumns, len(o))

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete all from deployer slice")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by deleteall for deployers")
	}

	if len(deployerAfterDeleteHooks) != 0 {
		for _, obj := range o {
			if err := obj.doAfterDeleteHooks(ctx, exec); err != nil {
				return 0, err
			}
		}
	}

	return rowsAff, nil
}

// Reload refetches the object from the database
// using the primary keys with an executor.
func (o *Deployer) Reload(ctx context.Context, exec boil.ContextExecutor) error {
	ret, err := FindDeployer(ctx, exec, o.OwnerAddress)
	if err != nil {
		return err
	}

	*o = *ret
	return nil
}

// ReloadAll refetches every row with matching primary key column values
// and overwrites the original object slice with the newly updated slice.
func (o *DeployerSlice) ReloadAll(ctx context.Context, exec boil.ContextExecutor) error {
	if o == nil || len(*o) == 0 {
		return nil
	}

	slice := DeployerSlice{}
	var args []interface{}
	for _, obj := range *o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), deployerPrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := "SELECT \"deployers\".* FROM \"deployers\" WHERE " +
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), 1, deployerPrimaryKeyColumns, len(*o))

	q := queries.Raw(sql, args...)

	err := q.Bind(ctx, exec, &slice)
	if err != nil {
		return errors.Wrap(err, "models: unable to reload all in DeployerSlice")
	}

	*o = slice

	return nil
}

// DeployerExists checks if the Deployer row exists.
func DeployerExists(ctx context.Context, exec boil.ContextExecutor, ownerAddress string) (bool, error) {
	var exists bool
	sql := "select exists(select 1 from \"deployers\" where \"owner_address\"=$1 limit 1)"

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, ownerAddress)
	}
	row := exec.QueryRowContext(ctx, sql, ownerAddress)

	err := row.Scan(&exists)
	if err != nil {
		return false, errors.Wrap(err, "models: unable to check if deployers exists")
	}

	return exists, nil
}

// Exists checks if the Deployer row exists.
func (o *Deployer) Exists(ctx context.Context, exec boil.ContextExecutor) (bool, error) {
	return DeployerExists(ctx, exec, o.OwnerAddress)
}
