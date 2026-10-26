package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Result зберігає результат опитування адреси (Рівень 1)
type Result struct {
	URL        string
	StatusCode int
	Duration   time.Duration
	Err        error
}

// HealthChecker - інтерфейс для абстрагування операції перевірки ресурсу (Рівень 1)
type HealthChecker interface {
	Check(url string) Result
}

// HTTPChecker реалізує інтерфейс HealthChecker за допомогою http.Client (Рівень 1)
type HTTPChecker struct {
	client *http.Client
}

// NewHTTPChecker створює новий екземпляр HTTPChecker з налаштованим таймаутом
func NewHTTPChecker(timeout time.Duration) *HTTPChecker {
	return &HTTPChecker{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Check виконує HTTP-запит до цільового URL та заміряє час виконання (Рівень 1)
func (c *HTTPChecker) Check(targetURL string) Result {
	start := time.Now()
	resp, err := c.client.Get(targetURL)
	duration := time.Since(start)

	if err != nil {
		return Result{
			URL:      targetURL,
			Duration: duration,
			Err:      err,
		}
	}
	defer resp.Body.Close()

	return Result{
		URL:        targetURL,
		StatusCode: resp.StatusCode,
		Duration:   duration,
		Err:        nil,
	}
}

// Statistics зберігає агреговану статистику моніторингу (Рівень 2 та 3)
type Statistics struct {
	Total      int
	Successful int
	Errors     int
	Status200  int
}

// CheckSequentially опитує адреси послідовно в одному потоці (Рівень 1)
func CheckSequentially(checker HealthChecker, urls []string) ([]Result, time.Duration) {
	start := time.Now()
	results := make([]Result, 0, len(urls))

	for _, u := range urls {
		res := checker.Check(u)
		results = append(results, res)
	}

	totalDuration := time.Since(start)
	return results, totalDuration
}

// CheckConcurrentMutex виконує перевірку за допомогою горутин та sync.Mutex (Рівень 2)
func CheckConcurrentMutex(checker HealthChecker, urls []string) (Statistics, time.Duration) {
	start := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var stats Statistics

	for _, u := range urls {
		wg.Add(1)
		// Передаємо поточну адресу u як аргумент анонімної функції для запобігання стану гонитви на замиканні
		go func(url string) {
			defer wg.Done()

			res := checker.Check(url)

			// Захист критичної секції спільних лічильників
			mu.Lock()
			stats.Total++
			if res.Err != nil {
				stats.Errors++
			} else {
				stats.Successful++
				if res.StatusCode == http.StatusOK {
					stats.Status200++
				}
			}
			mu.Unlock()
		}(u)
	}

	wg.Wait()
	totalDuration := time.Since(start)
	return stats, totalDuration
}

// CheckConcurrentChannels виконує перевірку адрес за допомогою горутин та каналу (Рівень 3)
func CheckConcurrentChannels(checker HealthChecker, urls []string, onResult func(Result)) (Statistics, time.Duration) {
	start := time.Now()

	// Створюємо буферизований канал результатів
	resultsChan := make(chan Result, len(urls))
	var wg sync.WaitGroup

	for _, u := range urls {
		wg.Add(1)
		// Передача аргументу url виключає data race на змінній циклу
		go func(url string) {
			defer wg.Done()
			res := checker.Check(url)
			resultsChan <- res
		}(u)
	}

	// Горутина-координатор очікує завершення всіх обчислень і безпечно закриває канал
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var stats Statistics
	// Вичитування всіх результатів із закритого каналу в головному потоці
	for res := range resultsChan {
		stats.Total++
		if res.Err != nil {
			stats.Errors++
		} else {
			stats.Successful++
			if res.StatusCode == http.StatusOK {
				stats.Status200++
			}
		}

		if onResult != nil {
			onResult(res)
		}
	}

	totalDuration := time.Since(start)
	return stats, totalDuration
}

// printResult виводить відформатований рядок результату перевірки адреси
func printResult(res Result) {
	if res.Err != nil {
		fmt.Printf("-> %s: Помилка (%v)\n", res.URL, res.Err)
	} else {
		statusText := http.StatusText(res.StatusCode)
		if statusText != "" {
			fmt.Printf("-> %s: Status %d %s (час: %v)\n", res.URL, res.StatusCode, statusText, res.Duration.Round(time.Millisecond))
		} else {
			fmt.Printf("-> %s: Status %d (час: %v)\n", res.URL, res.StatusCode, res.Duration.Round(time.Millisecond))
		}
	}
}

func main() {
	modeFlag := flag.String("mode", "all", "Режим роботи: seq (Рівень 1), mutex (Рівень 2), chan (Рівень 3), all (порівняння)")
	timeoutFlag := flag.Duration("timeout", 5*time.Second, "Таймаут для HTTP запитів")
	flag.Parse()

	// Перелік цільових адрес згідно з вимогами:
	// 2-3 реальні сайти, код зі статусом 404, неіснуючий домен для тестування помилок
	urls := []string{
		"https://go.dev",
		"https://github.com",
		"https://httpbin.org/status/404",
		"https://invalid-fake-host-test.org",
	}

	checker := NewHTTPChecker(*timeoutFlag)

	fmt.Println("=== HTTP URL Health Checker ===")

	switch *modeFlag {
	case "seq":
		fmt.Println("\n[Послідовна перевірка адрес (Рівень 1)...]")
		results, seqDur := CheckSequentially(checker, urls)
		for _, r := range results {
			printResult(r)
		}
		fmt.Printf("\nЧас послідовного опитування: %v\n", seqDur.Round(time.Millisecond))

	case "mutex":
		fmt.Println("\n[Конкурентна перевірка з Mutex (Рівень 2)...]")
		stats, mutexDur := CheckConcurrentMutex(checker, urls)
		fmt.Printf("Перевірено: %d, Успішних: %d, Помилок: %d (Час: %v)\n",
			stats.Total, stats.Successful, stats.Errors, mutexDur.Round(time.Millisecond))

	default:
		// Стандартний режим: демонстрація Рівня 3 та порівняльний аналіз швидкодії (Рівень 1 проти Рівня 3)
		// Спочатку заміряємо час послідовного прогону (Рівень 1)
		_, seqDuration := CheckSequentially(checker, urls)

		// Запускаємо конкурентний моніторинг через канали (Рівень 3)
		fmt.Println("\n[Конкурентна перевірка адрес...]")
		stats, concDuration := CheckConcurrentChannels(checker, urls, func(res Result) {
			printResult(res)
		})

		// Підсумковий структурований звіт моніторингу
		fmt.Println("==========================================")
		fmt.Println("ПІДСУМКОВИЙ ЗВІТ МОНІТОРИНГУ:")
		fmt.Printf("- Усього перевірено адрес: %d\n", stats.Total)
		fmt.Printf("- Успішних з'єднань: %d\n", stats.Successful)
		fmt.Printf("- Мережевих помилок: %d\n", stats.Errors)
		fmt.Printf("- Загальний час роботи: %v (проти %v у послідовному режимі)\n",
			concDuration.Round(time.Millisecond), seqDuration.Round(time.Millisecond))
		fmt.Println("==========================================")
	}
}
