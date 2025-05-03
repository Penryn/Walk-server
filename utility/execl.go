package utility

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"log"

	"errors"

	"github.com/xuri/excelize/v2"
)

// File excel 各单元数据结构
type File struct {
	Sheets []Sheet `json:"sheets"`
}

type Sheet struct {
	Name    string   `json:"name"`    // 工作表名称
	Headers []string `json:"headers"` // 列名
	Rows    [][]any  `json:"rows"`    // 行数据（支持不同类型）
}

// 常量定义
const (
	DefaultSheetName  = "Sheet1"
	DefaultDirPerm    = 0750
	DefaultHostSuffix = "/"
	MinColumnWidth    = 10  // 最小列宽
	MergeCheckColumns = 4   // 检查是否需要合并单元格的列数
	BatchSize         = 100 // 批量写入的行数
	DefaultRowCap     = 32  // 默认行容量
	DefaultBatchCap   = 100 // 默认批次容量
	MaxSheetNameLen   = 31  // Excel 工作表名称最大长度
	MaxColumnWidth    = 255 // Excel 最大列宽
)

// 错误定义
var (
	ErrInvalidFileData    = errors.New("无效的文件数据")
	ErrInitStyleFailed    = errors.New("初始化样式失败")
	ErrCreateSheetFailed  = errors.New("创建工作表失败")
	ErrWriteHeadersFailed = errors.New("写入列名失败")
	ErrWriteRowsFailed    = errors.New("写入数据行失败")
	ErrSetColumnWidth     = errors.New("设置列宽失败")
	ErrSaveFileFailed     = errors.New("保存文件失败")
	ErrCreateDirFailed    = errors.New("创建目录失败")
	ErrRemoveFileFailed   = errors.New("删除文件失败")
)

// 对象池
var (
	// 行数据对象池
	rowPool = sync.Pool{
		New: func() interface{} {
			return make([]interface{}, 0, DefaultRowCap)
		},
	}
	// 批次对象池
	batchPool = sync.Pool{
		New: func() interface{} {
			return make([][]interface{}, 0, DefaultBatchCap)
		},
	}
	// 单元格引用对象池
	cellRefPool = sync.Pool{
		New: func() interface{} {
			return make([]string, 0, BatchSize)
		},
	}
	// 列宽对象池
	columnWidthPool = sync.Pool{
		New: func() interface{} {
			return make([]int, 0, 32)
		},
	}
)

// 样式缓存
var (
	headerStyleID   int
	centeredStyleID int
)

// CreateExcelFile 生成 Excel 文件
func CreateExcelFile(data File, fileName, filePath, host string) (string, error) {
	if err := validateFileData(data); err != nil {
		return "", fmt.Errorf("无效的文件数据: %w", err)
	}

	f := excelize.NewFile()
	// 使用 defer 确保在任何情况下都能关闭文件
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("关闭 Excel 文件失败: %v", err)
		}
	}()

	// 初始化样式
	if err := initStyles(f); err != nil {
		return "", fmt.Errorf("初始化样式失败: %w", err)
	}

	// 处理每个工作表
	for i, sheet := range data.Sheets {
		if err := createSheet(f, sheet, i); err != nil {
			return "", fmt.Errorf("创建工作表 '%s' 失败: %w", sheet.Name, err)
		}
	}

	f.SetActiveSheet(0)

	fullPath := filepath.Join(filePath, fileName)
	if err := ensureDirExists(filePath); err != nil {
		return "", fmt.Errorf("确保目录存在失败: %w", err)
	}

	if err := removeOldFile(fullPath); err != nil {
		log.Printf("删除旧文件失败: %v", err)
	}

	if err := f.SaveAs(fullPath); err != nil {
		return "", fmt.Errorf("保存 Excel 文件失败: %w", err)
	}

	if !strings.HasSuffix(host, DefaultHostSuffix) {
		host += DefaultHostSuffix
	}

	return host + fullPath, nil
}

// initStyles 初始化样式
func initStyles(f *excelize.File) error {
	var err error
	headerStyleID, err = f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
	})
	if err != nil {
		return fmt.Errorf("创建列名样式失败: %w", err)
	}

	centeredStyleID, err = f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})
	if err != nil {
		return fmt.Errorf("创建居中对齐样式失败: %w", err)
	}

	return nil
}

// createSheet 创建一个工作表，并填充数据
func createSheet(f *excelize.File, sheet Sheet, index int) error {
	sheetName := sheet.Name
	if index == 0 {
		if err := f.SetSheetName(DefaultSheetName, sheetName); err != nil {
			return fmt.Errorf("重命名默认工作表失败: %w", err)
		}
	} else {
		if _, err := f.NewSheet(sheetName); err != nil {
			return fmt.Errorf("创建新工作表失败: %w", err)
		}
	}

	sw, err := f.NewStreamWriter(sheetName)
	if err != nil {
		return fmt.Errorf("创建流式写入器失败: %w", err)
	}

	// 使用 defer 确保在函数返回前刷新数据
	defer func() {
		if err := sw.Flush(); err != nil {
			log.Printf("刷新流式写入器失败: %v", err)
		}
	}()

	// 设置列宽
	if err := autoAdjustColumnWidth(sw, sheet.Headers, sheet.Rows); err != nil {
		return fmt.Errorf("设置列宽失败: %w", err)
	}

	// 写入列名
	if err := writeHeaders(sw, sheet.Headers); err != nil {
		return fmt.Errorf("写入列名失败: %w", err)
	}

	// 写入数据行
	if err := writeRows(sw, sheet.Rows); err != nil {
		return fmt.Errorf("写入数据行失败: %w", err)
	}

	// 确保所有数据都被刷新
	if err := sw.Flush(); err != nil {
		return fmt.Errorf("刷新工作表数据失败: %w", err)
	}

	return nil
}

// autoAdjustColumnWidth 自动调整列宽
func autoAdjustColumnWidth(sw *excelize.StreamWriter, headers []string, rows [][]any) error {
	// 从对象池获取列宽切片
	columnWidths := columnWidthPool.Get().([]int)
	defer func() {
		// 清空切片内容
		for i := range columnWidths {
			columnWidths[i] = 0
		}
		columnWidths = columnWidths[:0]
		columnWidthPool.Put(columnWidths)
	}()

	// 确保切片容量足够
	if cap(columnWidths) < len(headers) {
		columnWidths = make([]int, len(headers))
	} else {
		columnWidths = columnWidths[:len(headers)]
	}

	// 预计算所有列的最大宽度
	for colIndex, header := range headers {
		maxWidth := len(header)
		for _, row := range rows {
			if colIndex < len(row) {
				cellValue := fmt.Sprintf("%v", row[colIndex])
				cellWidth := len(cellValue)
				if cellWidth > maxWidth {
					maxWidth = cellWidth
				}
			}
		}
		// 确保最小列宽
		if maxWidth < MinColumnWidth {
			maxWidth = MinColumnWidth
		}
		columnWidths[colIndex] = maxWidth
	}

	// 一次性设置所有列宽
	for colIndex, width := range columnWidths {
		if err := sw.SetColWidth(colIndex+1, colIndex+1, float64(width+1)); err != nil {
			return fmt.Errorf("设置列宽失败，列索引: %d: %w", colIndex+1, err)
		}
	}

	return nil
}

// writeHeaders 写入列名到工作表
func writeHeaders(sw *excelize.StreamWriter, headers []string) error {
	cellRef, _ := excelize.CoordinatesToCellName(1, 1)

	// 从对象池获取行数据切片
	headerInterfaces := rowPool.Get().([]interface{})
	defer rowPool.Put(headerInterfaces)

	// 确保切片容量足够
	if cap(headerInterfaces) < len(headers) {
		headerInterfaces = make([]interface{}, len(headers))
	} else {
		headerInterfaces = headerInterfaces[:len(headers)]
	}

	// 填充列名数据
	for i, header := range headers {
		headerInterfaces[i] = header
	}

	if err := sw.SetRow(cellRef, headerInterfaces, excelize.RowOpts{StyleID: headerStyleID}); err != nil {
		return fmt.Errorf("设置列名行失败: %w", err)
	}

	return nil
}

// writeRows 写入数据行到工作表
func writeRows(sw *excelize.StreamWriter, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	numCols := len(rows[0])

	// 从对象池获取批次
	batch := batchPool.Get().([][]interface{})
	defer func() {
		// 清空批次并放回对象池
		for i := range batch {
			batch[i] = nil
		}
		batch = batch[:0]
		batchPool.Put(batch)
	}()

	// 从对象池获取单元格引用
	cellRefs := cellRefPool.Get().([]string)
	defer func() {
		cellRefs = cellRefs[:0]
		cellRefPool.Put(cellRefs)
	}()

	// 预计算单元格引用
	cellRefs = append(cellRefs, make([]string, len(rows))...)
	for i := range rows {
		cellRefs[i], _ = excelize.CoordinatesToCellName(1, i+2)
	}

	// 从对象池获取行数据切片
	rowInterfaces := rowPool.Get().([]interface{})
	defer func() {
		for i := range rowInterfaces {
			rowInterfaces[i] = nil
		}
		rowInterfaces = rowInterfaces[:0]
		rowPool.Put(rowInterfaces)
	}()

	// 确保切片容量足够
	if cap(rowInterfaces) < numCols {
		rowInterfaces = make([]interface{}, numCols)
	} else {
		rowInterfaces = rowInterfaces[:numCols]
	}

	for rowIndex, row := range rows {
		// 填充行数据
		for colIndex, cell := range row {
			c := excelize.Cell{Value: cell}
			if colIndex < MergeCheckColumns {
				c.StyleID = centeredStyleID
			}
			rowInterfaces[colIndex] = c
		}

		// 复制当前行数据到批次中
		rowCopy := make([]interface{}, numCols)
		copy(rowCopy, rowInterfaces)
		batch = append(batch, rowCopy)

		// 当批次达到指定大小时，批量写入
		if len(batch) >= BatchSize || rowIndex == len(rows)-1 {
			for i, rowData := range batch {
				cellRef := cellRefs[rowIndex-len(batch)+i+1]
				if err := sw.SetRow(cellRef, rowData); err != nil {
					return fmt.Errorf("批量写入数据行失败: %w", err)
				}
			}
			// 清空批次
			for i := range batch {
				batch[i] = nil
			}
			batch = batch[:0]
		}
	}

	return nil
}

// validateFileData 验证文件数据
func validateFileData(data File) error {
	if len(data.Sheets) == 0 {
		return fmt.Errorf("%w: 没有提供工作表", ErrInvalidFileData)
	}
	for _, sheet := range data.Sheets {
		if len(sheet.Name) == 0 {
			return fmt.Errorf("%w: 工作表名称不能为空", ErrInvalidFileData)
		}
		if len(sheet.Name) > MaxSheetNameLen {
			return fmt.Errorf("%w: 工作表名称超过最大长度 %d", ErrInvalidFileData, MaxSheetNameLen)
		}
		if len(sheet.Headers) == 0 {
			return fmt.Errorf("%w: 工作表 '%s' 没有列名", ErrInvalidFileData, sheet.Name)
		}
		if len(sheet.Rows) == 0 {
			return fmt.Errorf("%w: 工作表 '%s' 没有数据行", ErrInvalidFileData, sheet.Name)
		}
		// 检查所有行是否具有与列名相同的列数
		headerLen := len(sheet.Headers)
		for i, row := range sheet.Rows {
			if len(row) != headerLen {
				return fmt.Errorf("%w: 工作表 '%s' 第 %d 行长度与列名长度不匹配", ErrInvalidFileData, sheet.Name, i+1)
			}
		}
	}
	return nil
}

// ensureDirExists 确保目录存在
func ensureDirExists(filePath string) error {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := os.MkdirAll(filePath, DefaultDirPerm); err != nil {
			return fmt.Errorf("%w: %v", ErrCreateDirFailed, err)
		}
	}
	return nil
}

// removeOldFile 删除旧文件
func removeOldFile(fullPath string) error {
	if _, err := os.Stat(fullPath); err == nil {
		if err := os.Remove(fullPath); err != nil {
			return fmt.Errorf("%w: %v", ErrRemoveFileFailed, err)
		}
	}
	return nil
}
