// Package pipeline - пул воркеров для параллельной обработки изображений.
//
// Важное architектурное решение: повтор задачи при временной ошибке (retry)
// делается синхронно внутри той же горутины-воркера простым циклом с паузой,
// а НЕ через повторную отправку задачи обратно в канал jobQueue. Это сделано
// специально, чтобы исключить дедлок: если бы воркер сам себе отправлял
// задачу назад в канал, а канал оказался бы заполнен (например, все воркеры
// одновременно ушли в повтор) - никто не смог бы читать из канала, и все
// воркеры зависли бы навсегда. Синхронный retry этого риска не несёт.
package pipeline

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	apperrors "github.com/QtaroAXE/image-redactor/internal/domain/errors"
	"github.com/QtaroAXE/image-redactor/internal/domain/imginfo"
	compressor "github.com/QtaroAXE/image-redactor/internal/infra/codec"
	"github.com/QtaroAXE/image-redactor/internal/infra/fs"
)

// maxAttempts - сколько раз пробуем обработать задачу, прежде чем сдаться
// и переместить файл в директорию ошибок.
const maxAttempts = 3

// WorkerPool - пул горутин-воркеров, обрабатывающих очередь задач.
//
// ВАЖНО: AddJob предполагается вызывать только до Wait()/Stop(). Пул не
// рассчитан на добавление новых задач параллельно с его остановкой -
// в текущем консольном интерфейсе так и происходит (все задачи добавляются
// заранее, потом идёт ожидание и остановка).
type WorkerPool struct {
	workerCount int

	compressor *compressor.CompressorService
	fs         *fs.FileSystem

	jobQueue    chan Job
	resultQueue chan Result

	ctx    context.Context
	cancel context.CancelFunc

	// workersWG и resultWG специально разделены (а не одна общая WaitGroup):
	// resultProcessor не может завершиться, пока не закрыт resultQueue, а
	// resultQueue можно закрывать только после того, как ВСЕ воркеры (единственные,
	// кто в него пишет) точно закончили работу. Если ждать закрытия результатов
	// одной общей WaitGroup вместе с воркерами - получится циклическое ожидание
	// и Stop() зависнет навсегда.
	workersWG sync.WaitGroup
	resultWG  sync.WaitGroup

	jobsWG sync.WaitGroup // считает задачи, которые ещё не завершены (для Wait())

	stats   PoolStats
	statsMu sync.Mutex // защищает stats целиком; специально не используется atomic,
	// чтобы не было гонок на смеси atomic/mutex, как в исходной версии кода

	errorHandler func(error)
}

// Job - задача на обработку одного изображения.
type Job struct {
	ID         string
	Source     imginfo.SourceImage
	Target     imginfo.TargetImage
	Options    compressor.ProcessingOptions
	CreatedAt  time.Time
}

// Result - результат обработки одной задачи.
type Result struct {
	JobID       string
	SourcePath  string
	OutputPath  string
	Error       error
	Duration    time.Duration
	SizeBefore  int64
	SizeAfter   int64
	Success     bool
	ProcessedAt time.Time
}

// PoolStats - статистика пула для отображения прогресса.
type PoolStats struct {
	TotalJobs     int
	CompletedJobs int
	FailedJobs    int
	RetriedJobs   int
	StartTime     time.Time
}

// PoolConfig - настройки при создании пула.
type PoolConfig struct {
	WorkerCount  int
	QueueSize    int
	ErrorHandler func(error) // вызывается при неудачной обработке задачи (после исчерпания попыток)
}

// NewWorkerPool создаёт новый пул воркеров.
func NewWorkerPool(comp *compressor.CompressorService, filesystem *fs.FileSystem, cfg PoolConfig) *WorkerPool {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = cfg.WorkerCount * 2
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		workerCount:  cfg.WorkerCount,
		compressor:   comp,
		fs:           filesystem,
		jobQueue:     make(chan Job, cfg.QueueSize),
		resultQueue:  make(chan Result, cfg.QueueSize),
		ctx:          ctx,
		cancel:       cancel,
		stats:        PoolStats{StartTime: time.Now()},
		errorHandler: cfg.ErrorHandler,
	}
}

// Start запускает воркеров и обработчик результатов.
func (p *WorkerPool) Start() {
	log.Printf("Запуск пула из %d воркеров", p.workerCount)

	for i := 0; i < p.workerCount; i++ {
		p.workersWG.Add(1)
		go p.worker(i)
	}

	p.resultWG.Add(1)
	go p.resultProcessor()
}

// Stop останавливает пул. Нужно вызывать после того, как все задачи добавлены
// и Wait() завершился - иначе можно потерять задачи, добавленные после закрытия очереди.
func (p *WorkerPool) Stop() {
	log.Println("Остановка пула воркеров...")
	p.cancel()
	close(p.jobQueue)
	p.workersWG.Wait() // сначала ждём воркеров - только они пишут в resultQueue
	close(p.resultQueue)
	p.resultWG.Wait() // теперь можно безопасно ждать, пока resultProcessor дочитает очередь
	log.Println("Пул воркеров остановлен")
}

// AddJob добавляет задачу в очередь на обработку.
func (p *WorkerPool) AddJob(source imginfo.SourceImage, target imginfo.TargetImage, opts compressor.ProcessingOptions) error {
	select {
	case <-p.ctx.Done():
		return apperrors.New(apperrors.TypeInternal, "пул остановлен, новые задачи не принимаются")
	default:
	}

	job := Job{
		ID:        fmt.Sprintf("job_%d", time.Now().UnixNano()),
		Source:    source,
		Target:    target,
		Options:   opts,
		CreatedAt: time.Now(),
	}

	p.statsMu.Lock()
	p.stats.TotalJobs++
	p.statsMu.Unlock()

	p.jobsWG.Add(1)

	select {
	case p.jobQueue <- job:
		return nil
	case <-p.ctx.Done():
		p.jobsWG.Done()
		return apperrors.New(apperrors.TypeInternal, "пул остановлен, новые задачи не принимаются")
	}
}

// GetStats возвращает копию текущей статистики (безопасно вызывать в любой момент).
func (p *WorkerPool) GetStats() PoolStats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

// Wait блокируется, пока все добавленные задачи не будут обработаны
// (успешно или нет, включая исчерпанные повторы).
func (p *WorkerPool) Wait() {
	p.jobsWG.Wait()
}

// worker - тело горутины-воркера: читает задачи из очереди и обрабатывает их.
func (p *WorkerPool) worker(id int) {
	defer p.workersWG.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobQueue:
			if !ok {
				return
			}
			result := p.processJob(id, job)
			p.resultQueue <- result
			p.jobsWG.Done()
		}
	}
}

// processJob обрабатывает одну задачу, при временных ошибках повторяя
// попытку до maxAttempts раз с нарастающей паузой между попытками.
func (p *WorkerPool) processJob(workerID int, job Job) Result {
	start := time.Now()
	result := Result{JobID: job.ID, SourcePath: job.Source.Path(), ProcessedAt: time.Now()}

	if !p.fs.FileExists(job.Source.Path()) {
		result.Error = apperrors.New(apperrors.TypeNotFound, "исходный файл не найден").
			WithPath(job.Source.Path()).WithWorker(workerID)
		p.fs.MoveToError(job.Source.Path())
		p.incFailed()
		return result
	}

	sizeBefore, err := p.fs.GetFileSize(job.Source.Path())
	if err != nil {
		result.Error = err
		p.fs.MoveToError(job.Source.Path())
		p.incFailed()
		return result
	}
	result.SizeBefore = sizeBefore

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = p.compressor.CompressImage(job.Source, job.Target, job.Options)
		if lastErr == nil {
			break
		}
		if !isRetryableError(lastErr) || attempt == maxAttempts {
			break
		}
		p.incRetried()
		log.Printf("Повтор задачи %s (попытка %d) после ошибки: %v", job.ID, attempt, lastErr)
		time.Sleep(time.Duration(attempt) * time.Second)
	}

	if lastErr != nil {
		result.Error = lastErr
		p.fs.MoveToError(job.Source.Path())
		p.incFailed()
		return result
	}

	sizeAfter, err := p.fs.GetFileSize(job.Target.Path())
	if err == nil {
		result.SizeAfter = sizeAfter
	}
	result.OutputPath = job.Target.Path()
	result.Success = true
	result.Duration = time.Since(start)
	p.incCompleted()

	if err := p.fs.MoveToProcessed(job.Source.Path()); err != nil {
		log.Printf("Не удалось перенести обработанный файл: %v", err)
	}

	return result
}

// resultProcessor читает результаты и логирует их. Работает в единственном
// экземпляре, поэтому может безопасно печатать в лог без дополнительных блокировок.
func (p *WorkerPool) resultProcessor() {
	defer p.resultWG.Done()

	for result := range p.resultQueue {
		if result.Error != nil {
			if p.errorHandler != nil {
				p.errorHandler(result.Error)
			}
			log.Printf("Задача %s завершилась с ошибкой: %v", result.JobID, result.Error)
			continue
		}
		log.Printf("Задача %s выполнена за %v (%.1f%% сжатия)",
			result.JobID, result.Duration, reductionPercent(result.SizeBefore, result.SizeAfter))
	}
}

func (p *WorkerPool) incCompleted() {
	p.statsMu.Lock()
	p.stats.CompletedJobs++
	p.statsMu.Unlock()
}
func (p *WorkerPool) incFailed() {
	p.statsMu.Lock()
	p.stats.FailedJobs++
	p.statsMu.Unlock()
}
func (p *WorkerPool) incRetried() {
	p.statsMu.Lock()
	p.stats.RetriedJobs++
	p.statsMu.Unlock()
}

// isRetryableError решает, стоит ли повторять задачу при данной ошибке -
// только для ошибок, которые могут быть временными (ввод-вывод, таймаут).
func isRetryableError(err error) bool {
	appErr, ok := err.(*apperrors.AppError)
	if !ok {
		return false
	}
	switch appErr.Type {
	case apperrors.TypeIO, apperrors.TypeTimeout:
		return true
	default:
		return false
	}
}

// reductionPercent вычисляет процент уменьшения размера файла.
func reductionPercent(before, after int64) float64 {
	if before == 0 {
		return 0
	}
	return (1 - float64(after)/float64(before)) * 100
}
