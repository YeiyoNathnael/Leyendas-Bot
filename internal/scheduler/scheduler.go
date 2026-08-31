package scheduler

import (
	"context"
	"log"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/robfig/cron/v3"

	"leyendas-bot/internal/config"
	"leyendas-bot/internal/db"
	"leyendas-bot/internal/notion"
)

// Scheduler coordinates all background cron jobs and periodic polling tasks.
type Scheduler struct {
	cron         *cron.Cron
	bot          *gotgbot.Bot
	store        *db.Store
	notionClient *notion.Client
	cfg          *config.Config
}

// NewScheduler creates and configures the background scheduler.
func NewScheduler(
	b *gotgbot.Bot,
	store *db.Store,
	notionClient *notion.Client,
	cfg *config.Config,
) *Scheduler {
	c := cron.New(
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)

	s := &Scheduler{
		cron:         c,
		bot:          b,
		store:        store,
		notionClient: notionClient,
		cfg:          cfg,
	}

	s.registerJobs()
	return s
}

func (s *Scheduler) registerJobs() {
	// 1. Daily Standup Prompt (e.g. 9:00 AM weekdays)
	if s.cfg.StandupCron != "" {
		_, err := s.cron.AddFunc(s.cfg.StandupCron, s.runStandupPrompt)
		if err != nil {
			log.Printf("[WARN] Failed to register standup cron: %v", err)
		}
	}

	// 2. Daily Standup Digest (e.g. 3:00 PM weekdays)
	if s.cfg.StandupDigestCron != "" {
		_, err := s.cron.AddFunc(s.cfg.StandupDigestCron, s.compileStandupDigest)
		if err != nil {
			log.Printf("[WARN] Failed to register standup digest cron: %v", err)
		}
	}

	// 3. Periodic Deadline Evaluation (e.g. every 15 minutes)
	deadlineSpec := "@every " + s.cfg.DeadlineCheckInterval.String()
	_, err := s.cron.AddFunc(deadlineSpec, s.checkDeadlines)
	if err != nil {
		log.Printf("[WARN] Failed to register deadline cron: %v", err)
	}

	// 4. Periodic Notion Task Change Polling (e.g. every 5 minutes)
	pollSpec := "@every " + s.cfg.NotionPollInterval.String()
	_, err = s.cron.AddFunc(pollSpec, s.pollNotionTasks)
	if err != nil {
		log.Printf("[WARN] Failed to register notion polling cron: %v", err)
	}
}

// Start begins background job execution.
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Println("⏱️ Background cron scheduler started successfully")
}

// Stop halts the scheduler and waits for in-flight tasks to complete.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("⏱️ Background cron scheduler stopped cleanly")
}

// TriggerInitialSync seeds the task snapshot table on startup so historical tasks are not spammed.
func (s *Scheduler) TriggerInitialSync(ctx context.Context) {
	s.pollNotionTasks()
}
