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
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"github.com/volatiletech/sqlboiler/v4/queries/qmhelper"
	"github.com/volatiletech/strmangle"
)

// State is an object representing the database table.
type State struct {
	ID                           int       `boil:"id" json:"id" toml:"id" yaml:"id"`
	NetworkName                  string    `boil:"network_name" json:"network_name" toml:"network_name" yaml:"network_name"`
	LowestBlockNumber            int       `boil:"lowest_block_number" json:"lowest_block_number" toml:"lowest_block_number" yaml:"lowest_block_number"`
	HighestBlockNumber           int       `boil:"highest_block_number" json:"highest_block_number" toml:"highest_block_number" yaml:"highest_block_number"`
	EarliestValidatorPerformance null.Time `boil:"earliest_validator_performance" json:"earliest_validator_performance,omitempty" toml:"earliest_validator_performance" yaml:"earliest_validator_performance,omitempty"`
	LatestValidatorPerformance   null.Time `boil:"latest_validator_performance" json:"latest_validator_performance,omitempty" toml:"latest_validator_performance" yaml:"latest_validator_performance,omitempty"`

	R *stateR `boil:"-" json:"-" toml:"-" yaml:"-"`
	L stateL  `boil:"-" json:"-" toml:"-" yaml:"-"`
}

var StateColumns = struct {
	ID                           string
	NetworkName                  string
	LowestBlockNumber            string
	HighestBlockNumber           string
	EarliestValidatorPerformance string
	LatestValidatorPerformance   string
}{
	ID:                           "id",
	NetworkName:                  "network_name",
	LowestBlockNumber:            "lowest_block_number",
	HighestBlockNumber:           "highest_block_number",
	EarliestValidatorPerformance: "earliest_validator_performance",
	LatestValidatorPerformance:   "latest_validator_performance",
}

var StateTableColumns = struct {
	ID                           string
	NetworkName                  string
	LowestBlockNumber            string
	HighestBlockNumber           string
	EarliestValidatorPerformance string
	LatestValidatorPerformance   string
}{
	ID:                           "state.id",
	NetworkName:                  "state.network_name",
	LowestBlockNumber:            "state.lowest_block_number",
	HighestBlockNumber:           "state.highest_block_number",
	EarliestValidatorPerformance: "state.earliest_validator_performance",
	LatestValidatorPerformance:   "state.latest_validator_performance",
}

// Generated where

type whereHelpernull_Time struct{ field string }

func (w whereHelpernull_Time) EQ(x null.Time) qm.QueryMod {
	return qmhelper.WhereNullEQ(w.field, false, x)
}
func (w whereHelpernull_Time) NEQ(x null.Time) qm.QueryMod {
	return qmhelper.WhereNullEQ(w.field, true, x)
}
func (w whereHelpernull_Time) LT(x null.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.LT, x)
}
func (w whereHelpernull_Time) LTE(x null.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.LTE, x)
}
func (w whereHelpernull_Time) GT(x null.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.GT, x)
}
func (w whereHelpernull_Time) GTE(x null.Time) qm.QueryMod {
	return qmhelper.Where(w.field, qmhelper.GTE, x)
}

func (w whereHelpernull_Time) IsNull() qm.QueryMod    { return qmhelper.WhereIsNull(w.field) }
func (w whereHelpernull_Time) IsNotNull() qm.QueryMod { return qmhelper.WhereIsNotNull(w.field) }

var StateWhere = struct {
	ID                           whereHelperint
	NetworkName                  whereHelperstring
	LowestBlockNumber            whereHelperint
	HighestBlockNumber           whereHelperint
	EarliestValidatorPerformance whereHelpernull_Time
	LatestValidatorPerformance   whereHelpernull_Time
}{
	ID:                           whereHelperint{field: "\"state\".\"id\""},
	NetworkName:                  whereHelperstring{field: "\"state\".\"network_name\""},
	LowestBlockNumber:            whereHelperint{field: "\"state\".\"lowest_block_number\""},
	HighestBlockNumber:           whereHelperint{field: "\"state\".\"highest_block_number\""},
	EarliestValidatorPerformance: whereHelpernull_Time{field: "\"state\".\"earliest_validator_performance\""},
	LatestValidatorPerformance:   whereHelpernull_Time{field: "\"state\".\"latest_validator_performance\""},
}

// StateRels is where relationship names are stored.
var StateRels = struct {
}{}

// stateR is where relationships are stored.
type stateR struct {
}

// NewStruct creates a new relationship struct
func (*stateR) NewStruct() *stateR {
	return &stateR{}
}

// stateL is where Load methods for each relationship are stored.
type stateL struct{}

var (
	stateAllColumns            = []string{"id", "network_name", "lowest_block_number", "highest_block_number", "earliest_validator_performance", "latest_validator_performance"}
	stateColumnsWithoutDefault = []string{"network_name", "lowest_block_number", "highest_block_number"}
	stateColumnsWithDefault    = []string{"id", "earliest_validator_performance", "latest_validator_performance"}
	statePrimaryKeyColumns     = []string{"id"}
	stateGeneratedColumns      = []string{}
)

type (
	// StateSlice is an alias for a slice of pointers to State.
	// This should almost always be used instead of []State.
	StateSlice []*State
	// StateHook is the signature for custom State hook methods
	StateHook func(context.Context, boil.ContextExecutor, *State) error

	stateQuery struct {
		*queries.Query
	}
)

// Cache for insert, update and upsert
var (
	stateType                 = reflect.TypeOf(&State{})
	stateMapping              = queries.MakeStructMapping(stateType)
	statePrimaryKeyMapping, _ = queries.BindMapping(stateType, stateMapping, statePrimaryKeyColumns)
	stateInsertCacheMut       sync.RWMutex
	stateInsertCache          = make(map[string]insertCache)
	stateUpdateCacheMut       sync.RWMutex
	stateUpdateCache          = make(map[string]updateCache)
	stateUpsertCacheMut       sync.RWMutex
	stateUpsertCache          = make(map[string]insertCache)
)

var (
	// Force time package dependency for automated UpdatedAt/CreatedAt.
	_ = time.Second
	// Force qmhelper dependency for where clause generation (which doesn't
	// always happen)
	_ = qmhelper.Where
)

var stateAfterSelectMu sync.Mutex
var stateAfterSelectHooks []StateHook

var stateBeforeInsertMu sync.Mutex
var stateBeforeInsertHooks []StateHook
var stateAfterInsertMu sync.Mutex
var stateAfterInsertHooks []StateHook

var stateBeforeUpdateMu sync.Mutex
var stateBeforeUpdateHooks []StateHook
var stateAfterUpdateMu sync.Mutex
var stateAfterUpdateHooks []StateHook

var stateBeforeDeleteMu sync.Mutex
var stateBeforeDeleteHooks []StateHook
var stateAfterDeleteMu sync.Mutex
var stateAfterDeleteHooks []StateHook

var stateBeforeUpsertMu sync.Mutex
var stateBeforeUpsertHooks []StateHook
var stateAfterUpsertMu sync.Mutex
var stateAfterUpsertHooks []StateHook

// doAfterSelectHooks executes all "after Select" hooks.
func (o *State) doAfterSelectHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateAfterSelectHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeInsertHooks executes all "before insert" hooks.
func (o *State) doBeforeInsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateBeforeInsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterInsertHooks executes all "after Insert" hooks.
func (o *State) doAfterInsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateAfterInsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeUpdateHooks executes all "before Update" hooks.
func (o *State) doBeforeUpdateHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateBeforeUpdateHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterUpdateHooks executes all "after Update" hooks.
func (o *State) doAfterUpdateHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateAfterUpdateHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeDeleteHooks executes all "before Delete" hooks.
func (o *State) doBeforeDeleteHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateBeforeDeleteHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterDeleteHooks executes all "after Delete" hooks.
func (o *State) doAfterDeleteHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateAfterDeleteHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doBeforeUpsertHooks executes all "before Upsert" hooks.
func (o *State) doBeforeUpsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateBeforeUpsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// doAfterUpsertHooks executes all "after Upsert" hooks.
func (o *State) doAfterUpsertHooks(ctx context.Context, exec boil.ContextExecutor) (err error) {
	if boil.HooksAreSkipped(ctx) {
		return nil
	}

	for _, hook := range stateAfterUpsertHooks {
		if err := hook(ctx, exec, o); err != nil {
			return err
		}
	}

	return nil
}

// AddStateHook registers your hook function for all future operations.
func AddStateHook(hookPoint boil.HookPoint, stateHook StateHook) {
	switch hookPoint {
	case boil.AfterSelectHook:
		stateAfterSelectMu.Lock()
		stateAfterSelectHooks = append(stateAfterSelectHooks, stateHook)
		stateAfterSelectMu.Unlock()
	case boil.BeforeInsertHook:
		stateBeforeInsertMu.Lock()
		stateBeforeInsertHooks = append(stateBeforeInsertHooks, stateHook)
		stateBeforeInsertMu.Unlock()
	case boil.AfterInsertHook:
		stateAfterInsertMu.Lock()
		stateAfterInsertHooks = append(stateAfterInsertHooks, stateHook)
		stateAfterInsertMu.Unlock()
	case boil.BeforeUpdateHook:
		stateBeforeUpdateMu.Lock()
		stateBeforeUpdateHooks = append(stateBeforeUpdateHooks, stateHook)
		stateBeforeUpdateMu.Unlock()
	case boil.AfterUpdateHook:
		stateAfterUpdateMu.Lock()
		stateAfterUpdateHooks = append(stateAfterUpdateHooks, stateHook)
		stateAfterUpdateMu.Unlock()
	case boil.BeforeDeleteHook:
		stateBeforeDeleteMu.Lock()
		stateBeforeDeleteHooks = append(stateBeforeDeleteHooks, stateHook)
		stateBeforeDeleteMu.Unlock()
	case boil.AfterDeleteHook:
		stateAfterDeleteMu.Lock()
		stateAfterDeleteHooks = append(stateAfterDeleteHooks, stateHook)
		stateAfterDeleteMu.Unlock()
	case boil.BeforeUpsertHook:
		stateBeforeUpsertMu.Lock()
		stateBeforeUpsertHooks = append(stateBeforeUpsertHooks, stateHook)
		stateBeforeUpsertMu.Unlock()
	case boil.AfterUpsertHook:
		stateAfterUpsertMu.Lock()
		stateAfterUpsertHooks = append(stateAfterUpsertHooks, stateHook)
		stateAfterUpsertMu.Unlock()
	}
}

// One returns a single state record from the query.
func (q stateQuery) One(ctx context.Context, exec boil.ContextExecutor) (*State, error) {
	o := &State{}

	queries.SetLimit(q.Query, 1)

	err := q.Bind(ctx, exec, o)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "models: failed to execute a one query for state")
	}

	if err := o.doAfterSelectHooks(ctx, exec); err != nil {
		return o, err
	}

	return o, nil
}

// All returns all State records from the query.
func (q stateQuery) All(ctx context.Context, exec boil.ContextExecutor) (StateSlice, error) {
	var o []*State

	err := q.Bind(ctx, exec, &o)
	if err != nil {
		return nil, errors.Wrap(err, "models: failed to assign all query results to State slice")
	}

	if len(stateAfterSelectHooks) != 0 {
		for _, obj := range o {
			if err := obj.doAfterSelectHooks(ctx, exec); err != nil {
				return o, err
			}
		}
	}

	return o, nil
}

// Count returns the count of all State records in the query.
func (q stateQuery) Count(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	var count int64

	queries.SetSelect(q.Query, nil)
	queries.SetCount(q.Query)

	err := q.Query.QueryRowContext(ctx, exec).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to count state rows")
	}

	return count, nil
}

// Exists checks if the row exists in the table.
func (q stateQuery) Exists(ctx context.Context, exec boil.ContextExecutor) (bool, error) {
	var count int64

	queries.SetSelect(q.Query, nil)
	queries.SetCount(q.Query)
	queries.SetLimit(q.Query, 1)

	err := q.Query.QueryRowContext(ctx, exec).Scan(&count)
	if err != nil {
		return false, errors.Wrap(err, "models: failed to check if state exists")
	}

	return count > 0, nil
}

// States retrieves all the records using an executor.
func States(mods ...qm.QueryMod) stateQuery {
	mods = append(mods, qm.From("\"state\""))
	q := NewQuery(mods...)
	if len(queries.GetSelect(q)) == 0 {
		queries.SetSelect(q, []string{"\"state\".*"})
	}

	return stateQuery{q}
}

// FindState retrieves a single record by ID with an executor.
// If selectCols is empty Find will return all columns.
func FindState(ctx context.Context, exec boil.ContextExecutor, iD int, selectCols ...string) (*State, error) {
	stateObj := &State{}

	sel := "*"
	if len(selectCols) > 0 {
		sel = strings.Join(strmangle.IdentQuoteSlice(dialect.LQ, dialect.RQ, selectCols), ",")
	}
	query := fmt.Sprintf(
		"select %s from \"state\" where \"id\"=$1", sel,
	)

	q := queries.Raw(query, iD)

	err := q.Bind(ctx, exec, stateObj)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "models: unable to select from state")
	}

	if err = stateObj.doAfterSelectHooks(ctx, exec); err != nil {
		return stateObj, err
	}

	return stateObj, nil
}

// Insert a single record using an executor.
// See boil.Columns.InsertColumnSet documentation to understand column list inference for inserts.
func (o *State) Insert(ctx context.Context, exec boil.ContextExecutor, columns boil.Columns) error {
	if o == nil {
		return errors.New("models: no state provided for insertion")
	}

	var err error

	if err := o.doBeforeInsertHooks(ctx, exec); err != nil {
		return err
	}

	nzDefaults := queries.NonZeroDefaultSet(stateColumnsWithDefault, o)

	key := makeCacheKey(columns, nzDefaults)
	stateInsertCacheMut.RLock()
	cache, cached := stateInsertCache[key]
	stateInsertCacheMut.RUnlock()

	if !cached {
		wl, returnColumns := columns.InsertColumnSet(
			stateAllColumns,
			stateColumnsWithDefault,
			stateColumnsWithoutDefault,
			nzDefaults,
		)

		cache.valueMapping, err = queries.BindMapping(stateType, stateMapping, wl)
		if err != nil {
			return err
		}
		cache.retMapping, err = queries.BindMapping(stateType, stateMapping, returnColumns)
		if err != nil {
			return err
		}
		if len(wl) != 0 {
			cache.query = fmt.Sprintf("INSERT INTO \"state\" (\"%s\") %%sVALUES (%s)%%s", strings.Join(wl, "\",\""), strmangle.Placeholders(dialect.UseIndexPlaceholders, len(wl), 1, 1))
		} else {
			cache.query = "INSERT INTO \"state\" %sDEFAULT VALUES%s"
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
		return errors.Wrap(err, "models: unable to insert into state")
	}

	if !cached {
		stateInsertCacheMut.Lock()
		stateInsertCache[key] = cache
		stateInsertCacheMut.Unlock()
	}

	return o.doAfterInsertHooks(ctx, exec)
}

// Update uses an executor to update the State.
// See boil.Columns.UpdateColumnSet documentation to understand column list inference for updates.
// Update does not automatically update the record in case of default values. Use .Reload() to refresh the records.
func (o *State) Update(ctx context.Context, exec boil.ContextExecutor, columns boil.Columns) (int64, error) {
	var err error
	if err = o.doBeforeUpdateHooks(ctx, exec); err != nil {
		return 0, err
	}
	key := makeCacheKey(columns, nil)
	stateUpdateCacheMut.RLock()
	cache, cached := stateUpdateCache[key]
	stateUpdateCacheMut.RUnlock()

	if !cached {
		wl := columns.UpdateColumnSet(
			stateAllColumns,
			statePrimaryKeyColumns,
		)

		if !columns.IsWhitelist() {
			wl = strmangle.SetComplement(wl, []string{"created_at"})
		}
		if len(wl) == 0 {
			return 0, errors.New("models: unable to update state, could not build whitelist")
		}

		cache.query = fmt.Sprintf("UPDATE \"state\" SET %s WHERE %s",
			strmangle.SetParamNames("\"", "\"", 1, wl),
			strmangle.WhereClause("\"", "\"", len(wl)+1, statePrimaryKeyColumns),
		)
		cache.valueMapping, err = queries.BindMapping(stateType, stateMapping, append(wl, statePrimaryKeyColumns...))
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
		return 0, errors.Wrap(err, "models: unable to update state row")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by update for state")
	}

	if !cached {
		stateUpdateCacheMut.Lock()
		stateUpdateCache[key] = cache
		stateUpdateCacheMut.Unlock()
	}

	return rowsAff, o.doAfterUpdateHooks(ctx, exec)
}

// UpdateAll updates all rows with the specified column values.
func (q stateQuery) UpdateAll(ctx context.Context, exec boil.ContextExecutor, cols M) (int64, error) {
	queries.SetUpdate(q.Query, cols)

	result, err := q.Query.ExecContext(ctx, exec)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update all for state")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to retrieve rows affected for state")
	}

	return rowsAff, nil
}

// UpdateAll updates all rows with the specified column values, using an executor.
func (o StateSlice) UpdateAll(ctx context.Context, exec boil.ContextExecutor, cols M) (int64, error) {
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
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), statePrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := fmt.Sprintf("UPDATE \"state\" SET %s WHERE %s",
		strmangle.SetParamNames("\"", "\"", 1, colNames),
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), len(colNames)+1, statePrimaryKeyColumns, len(o)))

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args...)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to update all in state slice")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to retrieve rows affected all in update all state")
	}
	return rowsAff, nil
}

// Upsert attempts an insert using an executor, and does an update or ignore on conflict.
// See boil.Columns documentation for how to properly use updateColumns and insertColumns.
func (o *State) Upsert(ctx context.Context, exec boil.ContextExecutor, updateOnConflict bool, conflictColumns []string, updateColumns, insertColumns boil.Columns, opts ...UpsertOptionFunc) error {
	if o == nil {
		return errors.New("models: no state provided for upsert")
	}

	if err := o.doBeforeUpsertHooks(ctx, exec); err != nil {
		return err
	}

	nzDefaults := queries.NonZeroDefaultSet(stateColumnsWithDefault, o)

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

	stateUpsertCacheMut.RLock()
	cache, cached := stateUpsertCache[key]
	stateUpsertCacheMut.RUnlock()

	var err error

	if !cached {
		insert, _ := insertColumns.InsertColumnSet(
			stateAllColumns,
			stateColumnsWithDefault,
			stateColumnsWithoutDefault,
			nzDefaults,
		)

		update := updateColumns.UpdateColumnSet(
			stateAllColumns,
			statePrimaryKeyColumns,
		)

		if updateOnConflict && len(update) == 0 {
			return errors.New("models: unable to upsert state, could not build update column list")
		}

		ret := strmangle.SetComplement(stateAllColumns, strmangle.SetIntersect(insert, update))

		conflict := conflictColumns
		if len(conflict) == 0 && updateOnConflict && len(update) != 0 {
			if len(statePrimaryKeyColumns) == 0 {
				return errors.New("models: unable to upsert state, could not build conflict column list")
			}

			conflict = make([]string, len(statePrimaryKeyColumns))
			copy(conflict, statePrimaryKeyColumns)
		}
		cache.query = buildUpsertQueryPostgres(dialect, "\"state\"", updateOnConflict, ret, update, conflict, insert, opts...)

		cache.valueMapping, err = queries.BindMapping(stateType, stateMapping, insert)
		if err != nil {
			return err
		}
		if len(ret) != 0 {
			cache.retMapping, err = queries.BindMapping(stateType, stateMapping, ret)
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
		return errors.Wrap(err, "models: unable to upsert state")
	}

	if !cached {
		stateUpsertCacheMut.Lock()
		stateUpsertCache[key] = cache
		stateUpsertCacheMut.Unlock()
	}

	return o.doAfterUpsertHooks(ctx, exec)
}

// Delete deletes a single State record with an executor.
// Delete will match against the primary key column to find the record to delete.
func (o *State) Delete(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if o == nil {
		return 0, errors.New("models: no State provided for delete")
	}

	if err := o.doBeforeDeleteHooks(ctx, exec); err != nil {
		return 0, err
	}

	args := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(o)), statePrimaryKeyMapping)
	sql := "DELETE FROM \"state\" WHERE \"id\"=$1"

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args...)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete from state")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by delete for state")
	}

	if err := o.doAfterDeleteHooks(ctx, exec); err != nil {
		return 0, err
	}

	return rowsAff, nil
}

// DeleteAll deletes all matching rows.
func (q stateQuery) DeleteAll(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if q.Query == nil {
		return 0, errors.New("models: no stateQuery provided for delete all")
	}

	queries.SetDelete(q.Query)

	result, err := q.Query.ExecContext(ctx, exec)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete all from state")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by deleteall for state")
	}

	return rowsAff, nil
}

// DeleteAll deletes all rows in the slice, using an executor.
func (o StateSlice) DeleteAll(ctx context.Context, exec boil.ContextExecutor) (int64, error) {
	if len(o) == 0 {
		return 0, nil
	}

	if len(stateBeforeDeleteHooks) != 0 {
		for _, obj := range o {
			if err := obj.doBeforeDeleteHooks(ctx, exec); err != nil {
				return 0, err
			}
		}
	}

	var args []interface{}
	for _, obj := range o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), statePrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := "DELETE FROM \"state\" WHERE " +
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), 1, statePrimaryKeyColumns, len(o))

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, args)
	}
	result, err := exec.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, errors.Wrap(err, "models: unable to delete all from state slice")
	}

	rowsAff, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "models: failed to get rows affected by deleteall for state")
	}

	if len(stateAfterDeleteHooks) != 0 {
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
func (o *State) Reload(ctx context.Context, exec boil.ContextExecutor) error {
	ret, err := FindState(ctx, exec, o.ID)
	if err != nil {
		return err
	}

	*o = *ret
	return nil
}

// ReloadAll refetches every row with matching primary key column values
// and overwrites the original object slice with the newly updated slice.
func (o *StateSlice) ReloadAll(ctx context.Context, exec boil.ContextExecutor) error {
	if o == nil || len(*o) == 0 {
		return nil
	}

	slice := StateSlice{}
	var args []interface{}
	for _, obj := range *o {
		pkeyArgs := queries.ValuesFromMapping(reflect.Indirect(reflect.ValueOf(obj)), statePrimaryKeyMapping)
		args = append(args, pkeyArgs...)
	}

	sql := "SELECT \"state\".* FROM \"state\" WHERE " +
		strmangle.WhereClauseRepeated(string(dialect.LQ), string(dialect.RQ), 1, statePrimaryKeyColumns, len(*o))

	q := queries.Raw(sql, args...)

	err := q.Bind(ctx, exec, &slice)
	if err != nil {
		return errors.Wrap(err, "models: unable to reload all in StateSlice")
	}

	*o = slice

	return nil
}

// StateExists checks if the State row exists.
func StateExists(ctx context.Context, exec boil.ContextExecutor, iD int) (bool, error) {
	var exists bool
	sql := "select exists(select 1 from \"state\" where \"id\"=$1 limit 1)"

	if boil.IsDebug(ctx) {
		writer := boil.DebugWriterFrom(ctx)
		fmt.Fprintln(writer, sql)
		fmt.Fprintln(writer, iD)
	}
	row := exec.QueryRowContext(ctx, sql, iD)

	err := row.Scan(&exists)
	if err != nil {
		return false, errors.Wrap(err, "models: unable to check if state exists")
	}

	return exists, nil
}

// Exists checks if the State row exists.
func (o *State) Exists(ctx context.Context, exec boil.ContextExecutor) (bool, error) {
	return StateExists(ctx, exec, o.ID)
}
