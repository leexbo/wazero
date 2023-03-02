package binary

import (
	"bytes"
	"fmt"
	"io"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/leb128"
	"github.com/tetratelabs/wazero/internal/wasm"
)

func decodeTypeSection(enabledFeatures api.CoreFeatures, r *bytes.Reader) ([]*wasm.FunctionType, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]*wasm.FunctionType, vs)
	for i := uint32(0); i < vs; i++ {
		if result[i], err = decodeFunctionType(enabledFeatures, r); err != nil {
			return nil, fmt.Errorf("read %d-th type: %v", i, err)
		}
	}
	return result, nil
}

func decodeImportSection(
	r *bytes.Reader,
	memorySizer memorySizer,
	memoryLimitPages uint32,
	enabledFeatures api.CoreFeatures,
) ([]*wasm.Import, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]*wasm.Import, vs)
	for i := uint32(0); i < vs; i++ {
		if result[i], err = decodeImport(r, i, memorySizer, memoryLimitPages, enabledFeatures); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func decodeFunctionSection(r *bytes.Reader) ([]uint32, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]uint32, vs)
	for i := uint32(0); i < vs; i++ {
		if result[i], _, err = leb128.DecodeUint32(r); err != nil {
			return nil, fmt.Errorf("get type index: %w", err)
		}
	}
	return result, err
}

func decodeTableSection(r *bytes.Reader, enabledFeatures api.CoreFeatures) ([]*wasm.Table, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("error reading size")
	}
	if vs > 1 {
		if err := enabledFeatures.RequireEnabled(api.CoreFeatureReferenceTypes); err != nil {
			return nil, fmt.Errorf("at most one table allowed in module as %w", err)
		}
	}

	ret := make([]*wasm.Table, vs)
	for i := range ret {
		table, err := decodeTable(r, enabledFeatures)
		if err != nil {
			return nil, err
		}
		ret[i] = table
	}
	return ret, nil
}

func decodeMemorySection(
	r *bytes.Reader,
	memorySizer memorySizer,
	memoryLimitPages uint32,
) (*wasm.Memory, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("error reading size")
	}
	if vs > 1 {
		return nil, fmt.Errorf("at most one memory allowed in module, but read %d", vs)
	} else if vs == 0 {
		// memory count can be zero.
		return nil, nil
	}

	return decodeMemory(r, memorySizer, memoryLimitPages)
}

func decodeGlobalSection(r *bytes.Reader, enabledFeatures api.CoreFeatures) ([]*wasm.Global, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]*wasm.Global, vs)
	for i := uint32(0); i < vs; i++ {
		if result[i], err = decodeGlobal(r, enabledFeatures); err != nil {
			return nil, fmt.Errorf("global[%d]: %w", i, err)
		}
	}
	return result, nil
}

func decodeExportSection(r *bytes.Reader) ([]*wasm.Export, error) {
	vs, _, sizeErr := leb128.DecodeUint32(r)
	if sizeErr != nil {
		return nil, fmt.Errorf("get size of vector: %v", sizeErr)
	}

	usedName := make(map[string]struct{}, vs)
	exportSection := make([]*wasm.Export, 0, vs)
	for i := wasm.Index(0); i < vs; i++ {
		export, err := decodeExport(r)
		if err != nil {
			return nil, fmt.Errorf("read export: %w", err)
		}
		if _, ok := usedName[export.Name]; ok {
			return nil, fmt.Errorf("export[%d] duplicates name %q", i, export.Name)
		} else {
			usedName[export.Name] = struct{}{}
		}
		exportSection = append(exportSection, export)
	}
	return exportSection, nil
}

func decodeStartSection(r *bytes.Reader) (*wasm.Index, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get function index: %w", err)
	}
	return &vs, nil
}

func decodeElementSection(r *bytes.Reader, enabledFeatures api.CoreFeatures) ([]*wasm.ElementSegment, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]*wasm.ElementSegment, vs)
	for i := uint32(0); i < vs; i++ {
		if result[i], err = decodeElementSegment(r, enabledFeatures); err != nil {
			return nil, fmt.Errorf("read element: %w", err)
		}
	}
	return result, nil
}

func decodeCodeSection(r *bytes.Reader) ([]*wasm.Code, error) {
	codeSectionStart := uint64(r.Len())
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]*wasm.Code, vs)
	for i := uint32(0); i < vs; i++ {
		c, err := decodeCode(r, codeSectionStart)
		if err != nil {
			return nil, fmt.Errorf("read %d-th code segment: %v", i, err)
		}
		result[i] = c
	}
	return result, nil
}

func decodeDataSection(r *bytes.Reader, enabledFeatures api.CoreFeatures) ([]*wasm.DataSegment, error) {
	vs, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get size of vector: %w", err)
	}

	result := make([]*wasm.DataSegment, vs)
	for i := uint32(0); i < vs; i++ {
		if result[i], err = decodeDataSegment(r, enabledFeatures); err != nil {
			return nil, fmt.Errorf("read data segment: %w", err)
		}
	}
	return result, nil
}

func decodeDataCountSection(r *bytes.Reader) (count *uint32, err error) {
	v, _, err := leb128.DecodeUint32(r)
	if err != nil && err != io.EOF {
		// data count is optional, so EOF is fine.
		return nil, err
	}
	return &v, nil
}
