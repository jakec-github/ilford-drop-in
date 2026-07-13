#!/bin/bash

claude --permission-mode auto --model opus "1. You are working on $1 \
2. Find the next open sub-issue
3. /implement it
ONLY DO ONE TASK"
