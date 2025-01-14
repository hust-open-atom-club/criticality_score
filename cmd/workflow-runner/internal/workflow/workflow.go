package workflow

import (
	"fmt"
	"os"
	"time"

	"github.com/HUSTSecLab/criticality_score/pkg/logger"
)

type WorkflowNode struct {
	Name         string
	Description  string
	NeedUpdate   func() bool
	RunBeforeCmd func() error
	Cmd          []string
	RunAfterCmd  func() error
	LogPrefix    string
	Dependencies []*WorkflowNode
}

func (n *WorkflowNode) newRunnintCtx(handler *runningHandler, opt *WorkflowStartOption) (*runningCtx, error) {
	// make sure the output dir exists
	err := os.MkdirAll(opt.OutputDir, 0755)
	if err != nil {
		logger.Error("failed to create output dir", opt.OutputDir)
		return nil, err
	}

	logFilename := opt.OutputDir + "/" + opt.OutputFileNameFn(n)

	logfile, err := os.OpenFile(logFilename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

	if err != nil {
		logger.Error("failed to open log file", n.Name)
		return nil, err
	}

	logger.Infof("Running %s, log file will save at %s", n.Name, logFilename)

	return &runningCtx{
		loggerFile:     logfile,
		flow:           n,
		runningHandler: handler,
	}, nil
}

const (
	DefaultOutputDir = "logs"
)

func DefaultOutputFileNameFn(w *WorkflowNode) string {
	return w.Name + "_" + time.Now().Format("2006-01-02-15-04-05") + ".log"
}

type WorkflowStartOption struct {
	OutputDir         string
	OutputFileNameFn  func(w *WorkflowNode) string
	NeedUpdateDefault bool
}

func (n *WorkflowNode) StartWorkflow(opt *WorkflowStartOption) (RunningHandler, error) {
	if opt == nil {
		opt = &WorkflowStartOption{
			OutputDir:         DefaultOutputDir,
			OutputFileNameFn:  DefaultOutputFileNameFn,
			NeedUpdateDefault: false,
		}
	}
	if opt.OutputDir == "" {
		opt.OutputDir = DefaultOutputDir
	}
	if opt.OutputFileNameFn == nil {
		opt.OutputFileNameFn = DefaultOutputFileNameFn
	}

	handler := newRunningHandler()

	go func() {
		sequence, err := caculateBuildSequence(n, opt.NeedUpdateDefault)
		if err != nil {
			logger.Errorf("Failed to build running sequence %v", err)
			handler.finish <- err
			return
		}

		logger.Infof("Starting workflow `%s` ...", n.Name)
		logger.Info("Following tasks will be run: ")

		for i, nodes := range sequence {
			logger.Infof("== Step %d", i)
			for _, node := range nodes {
				logger.Infof("    - %s", node.Name)
			}
		}

		for _, stepNodes := range sequence {

			// TODO: run in parallel
			for _, node := range stepNodes {
				if node.RunBeforeCmd == nil && node.RunAfterCmd == nil && node.Cmd == nil {
					logger.Infof("Skip %s", node.Name)
					continue
				}

				ctx, err := node.newRunnintCtx(handler, opt)
				if err != nil {
					logger.Errorf("Failed to create running context %v", err)
					handler.finish <- err
					return
				}
				err = ctx.Run()
				if err != nil {
					logger.Errorf("Failed to run %s %v", node.Name, err)
					handler.finish <- err
					return
				}
			}
		}

		logger.Infof("Workflow `%s` finished", n.Name)
		handler.finish <- nil
	}()

	return handler, nil
}

// Returns a 2-dimensional array, every element in the array is a sequence of nodes that can be run in parallel
//
// defaultNeedUpdate: if a node does not have a NeedUpdate function, use this value
func caculateBuildSequence(node *WorkflowNode, defaultNeedUpdate bool) ([][]*WorkflowNode, error) {
	visited := make(map[*WorkflowNode]bool)
	graph := make(map[*WorkflowNode][]*WorkflowNode)
	buildGraph(node, graph, visited)
	visited = make(map[*WorkflowNode]bool)
	indegree := make(map[*WorkflowNode]int)
	buildInDegree(node, indegree, visited)

	indiredNeedUpdate := make(map[*WorkflowNode]bool)

	result := make([][]*WorkflowNode, 0)

	for len(graph) > 0 {
		// nodes with out degree 0 in this round
		roundWorkflows := make([]*WorkflowNode, 0)
		for node, _ := range graph {
			if indegree[node] == 0 {
				roundWorkflows = append(roundWorkflows, node)
			}
		}
		if len(roundWorkflows) == 0 {
			return nil, fmt.Errorf("circular dependency detected")
		}

		roundNeedUpdateWorkflows := make([]*WorkflowNode, 0)
		for _, node := range roundWorkflows {
			if indiredNeedUpdate[node] {
				// if this node is tainted by others, it should be updated
				// and there is no need to taint others again
				// because nodes it can reach are already tainted
				roundNeedUpdateWorkflows = append(roundNeedUpdateWorkflows, node)
			} else {
				needUpdate := defaultNeedUpdate
				if node.NeedUpdate != nil {
					needUpdate = node.NeedUpdate()
				}
				if needUpdate {
					roundNeedUpdateWorkflows = append(roundNeedUpdateWorkflows, node)
					visited = make(map[*WorkflowNode]bool)
					taintOthers(node, graph, indiredNeedUpdate, visited)
				}
			}

			for _, tonode := range graph[node] {
				indegree[tonode]--
			}
			delete(graph, node)
		}
		if len(roundNeedUpdateWorkflows) != 0 {
			result = append(result, roundNeedUpdateWorkflows)
		}
	}
	return result, nil
}

func buildInDegree(node *WorkflowNode, indegree map[*WorkflowNode]int, visited map[*WorkflowNode]bool) {
	if _, ok := visited[node]; ok {
		return
	}
	visited[node] = true
	indegree[node] = len(node.Dependencies)
	for _, dep := range node.Dependencies {
		buildInDegree(dep, indegree, visited)
	}
}

func buildGraph(node *WorkflowNode, graph map[*WorkflowNode][]*WorkflowNode, visited map[*WorkflowNode]bool) {
	if _, ok := visited[node]; ok {
		return
	}
	visited[node] = true

	if _, ok := graph[node]; !ok {
		graph[node] = make([]*WorkflowNode, 0)
	}

	if node.Dependencies == nil {
		return
	}

	for _, dep := range node.Dependencies {
		graph[dep] = append(graph[dep], node)
		buildGraph(dep, graph, visited)
	}
}

func taintOthers(node *WorkflowNode, graph map[*WorkflowNode][]*WorkflowNode, indiredNeedUpdate map[*WorkflowNode]bool, visited map[*WorkflowNode]bool) {
	if _, ok := visited[node]; ok {
		return
	}
	visited[node] = true

	indiredNeedUpdate[node] = true
	for _, dep := range graph[node] {
		taintOthers(dep, graph, indiredNeedUpdate, visited)
	}
}