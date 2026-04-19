#!/bin/sh
# BUG-019: Bug node auto-create with no affected files
#
# NOT A BUG — expected behavior per TASK-226 unconditional-upsert decision.
# MergeBugAffects always creates a Bug node even when no --files/--symbols are
# provided. A Bug node with no AFFECTS edges is valid: it means the reporter
# didn't specify scope yet, not that the code is broken.
#
# This script serves as documentation only. Exit 0 confirms the "wontfix"
# assessment: there is no failure mode to guard against.
exit 0
