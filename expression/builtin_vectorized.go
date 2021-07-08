// Copyright 2019 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package expression

import (
	"sync"

	"github.com/pingcap/errors"
	"github.com/pingcap/parser/mysql"
	"github.com/pingcap/tidb/types"
	"github.com/pingcap/tidb/util/chunk"
)

// columnBufferAllocator is used to allocate and release column buffer in vectorized evaluation.
type columnBufferAllocator interface {
	// get allocates a column buffer with the specific eval type and capacity.
	// the allocator is not responsible for initializing the column, so please initialize it before using.
	get(evalType types.EvalType, capacity int) (*chunk.Column, error)
	// put releases a column buffer.
	put(buf *chunk.Column)
}

// localSliceBuffer implements columnBufferAllocator interface.
// It works like a concurrency-safe deque which is implemented by a lock + slice.
type localSliceBuffer struct {
	pool sync.Pool
}

func newLocalSliceBuffer(initCap int) *localSliceBuffer {
	return &localSliceBuffer{
		pool: sync.Pool{
			New: func() interface{} {
				// Use default arguments
				col, _ := newBuffer(types.EvalType(mysql.TypeLonglong), chunk.InitialCapacity)
				return col
			},
		},
	}
}

var globalColumnAllocator = newLocalSliceBuffer(1024)

func newBuffer(evalType types.EvalType, capacity int) (*chunk.Column, error) {
	switch evalType {
	case types.ETInt:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeLonglong), capacity), nil
	case types.ETReal:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeDouble), capacity), nil
	case types.ETDecimal:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeNewDecimal), capacity), nil
	case types.ETDuration:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeDuration), capacity), nil
	case types.ETDatetime, types.ETTimestamp:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeDatetime), capacity), nil
	case types.ETString:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeString), capacity), nil
	case types.ETJson:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeJSON), capacity), nil
	}
	return nil, errors.Errorf("get column buffer for unsupported EvalType=%v", evalType)
}

// GetColumn allocates a column buffer with the specific eval type and capacity.
// the allocator is not responsible for initializing the column, so please initialize it before using.
func GetColumn(evalType types.EvalType, capacity int) (*chunk.Column, error) {
	return globalColumnAllocator.get(evalType, capacity)
}

// PutColumn releases a column buffer.
func PutColumn(buf *chunk.Column) {
	globalColumnAllocator.put(buf)
}

func (r *localSliceBuffer) get(evalType types.EvalType, capacity int) (*chunk.Column, error) {
	if col, ok := r.pool.Get().(*chunk.Column); ok {
		return col, nil
	}
	return newBuffer(evalType, capacity)
}

func (r *localSliceBuffer) put(col *chunk.Column) {
	r.pool.Put(col)
}

// vecEvalIntByRows uses the non-vectorized(row-based) interface `evalInt` to eval the expression.
func vecEvalIntByRows(sig builtinFunc, input *chunk.Chunk, result *chunk.Column) error {
	n := input.NumRows()
	result.ResizeInt64(n, false)
	i64s := result.Int64s()
	for i := 0; i < n; i++ {
		res, isNull, err := sig.evalInt(input.GetRow(i))
		if err != nil {
			return err
		}
		result.SetNull(i, isNull)
		i64s[i] = res
	}
	return nil
}

// vecEvalStringByRows uses the non-vectorized(row-based) interface `evalString` to eval the expression.
func vecEvalStringByRows(sig builtinFunc, input *chunk.Chunk, result *chunk.Column) error {
	n := input.NumRows()
	result.ReserveString(n)
	for i := 0; i < n; i++ {
		res, isNull, err := sig.evalString(input.GetRow(i))
		if err != nil {
			return err
		}
		if isNull {
			result.AppendNull()
			continue
		}
		result.AppendString(res)
	}
	return nil
}
