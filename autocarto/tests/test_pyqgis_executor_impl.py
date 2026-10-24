import threading
import unittest
from typing import Any, Dict, cast
from unittest.mock import patch

from autocarto.tools.system._shared.pyqgis_executor_impl import ScriptExecutor, ScriptTask, ScriptStatus
import autocarto.tools.system._shared.pyqgis_executor_impl as pyqgis_executor_impl
from autocarto.tools.system._shared.pyqgis_tools_impl import (
    pyqgis_get_task_status,
    pyqgis_submit_script,
)
import autocarto.tools.system._shared.pyqgis_tools_impl as pyqgis_tools_impl


class ScriptExecutorContractTestCase(unittest.TestCase):
    def setUp(self):
        pyqgis_tools_impl._executor = None
        pyqgis_tools_impl._validator = None

    def tearDown(self):
        pyqgis_tools_impl._executor = None
        pyqgis_tools_impl._validator = None

    def test_cancel_requested_blocks_late_progress_and_finishes_canceled(self):
        executor = ScriptExecutor(max_concurrent=1)
        observed_progress = []
        observed_states = []

        def on_progress(task_id, progress, message):
            observed_progress.append((progress, message))
            if progress == 0.25:
                self.assertTrue(executor.cancel_task(task_id))
                observed_states.append(executor.get_task_status(task_id))

        task_id = executor.submit(
            "report_progress(0.25, '第一步')\n"
            "report_progress(0.9, '取消后的晚到进度')\n"
            "result = 'done'\n",
            progress_callback=on_progress,
        )

        result = executor.wait_for_task(task_id, timeout=5)

        self.assertIsNotNone(result)
        self.assertTrue(observed_states)
        result = cast(Dict[str, Any], result)
        self.assertEqual(observed_states[0]["status"], "cancel_requested")
        self.assertEqual(result["status"], "canceled")
        self.assertEqual(result["progress"], 0.25)
        self.assertEqual(result["message"], "第一步")
        self.assertEqual(
            observed_progress, [(0.0, "开始执行脚本..."), (0.25, "第一步")]
        )

    def test_pyqgis_get_task_status_exposes_succeeded_public_state(self):
        pyqgis_tools_impl._executor = ScriptExecutor(max_concurrent=1)

        submit_result = pyqgis_submit_script({"script": "result = 'ok'"})

        self.assertTrue(submit_result["success"])
        task_id = submit_result["output"]
        pyqgis_tools_impl._executor.wait_for_task(task_id, timeout=5)
        status_result = pyqgis_get_task_status({"task_id": task_id})

        self.assertTrue(status_result["success"])
        self.assertEqual(status_result["info"], "succeeded")
        self.assertIn("'status': 'succeeded'", status_result["output"])

    def test_script_executor_uses_main_thread_runner_when_available(self):
        executor = ScriptExecutor(max_concurrent=1)
        runner_calls = []

        def fake_runner(fn):
            runner_calls.append(True)
            return fn()

        with patch.object(
            ScriptExecutor, "_resolve_main_thread_runner", return_value=fake_runner
        ):
            task_id = executor.submit("result = 'ok'")
            task = executor.wait_for_task(task_id, timeout=5)

        self.assertIsNotNone(task)
        task_dict = cast(Dict[str, Any], task)
        self.assertEqual(task_dict["status"], "succeeded")
        self.assertEqual(task_dict["result"], "ok")
        self.assertEqual(len(runner_calls), 1)

    def test_terminal_state_publishes_result_error_and_completion_together(self):
        for script_result, terminal, expected_result, expected_error in (
            ({"result": "ok"}, ScriptStatus.SUCCEEDED, "ok", None),
            ({"error": "broken"}, ScriptStatus.FAILED, None, "broken"),
            ({"timeout": True}, ScriptStatus.FAILED, None, "脚本执行超时"),
        ):
            with self.subTest(terminal=terminal, script_result=script_result):
                publishing = threading.Event()
                release = threading.Event()
                observed = []

                class ObservedTask(ScriptTask):
                    def __setattr__(self, name, value):
                        if name == "status" and value in (
                            ScriptStatus.SUCCEEDED,
                            ScriptStatus.FAILED,
                            ScriptStatus.CANCELED,
                        ):
                            # Freeze the writer at the public terminal transition.
                            # Readers must never observe terminal status with a
                            # result/error/completion timestamp still unpublished.
                            observed.append((self.result, self.error, self.completed_at))
                            publishing.set()
                            if not release.wait(timeout=5):
                                raise RuntimeError("terminal publication was not released")
                        super().__setattr__(name, value)

                executor = ScriptExecutor(max_concurrent=1)
                with patch.object(pyqgis_executor_impl, "ScriptTask", ObservedTask), patch.object(
                    executor, "_run_script_with_timeout", return_value=script_result
                ):
                    task_id = executor.submit("result = 'ok'")
                    try:
                        self.assertTrue(publishing.wait(timeout=5))
                        self.assertEqual(len(observed), 1)
                        result, error, completed_at = observed[0]
                        self.assertEqual(result, expected_result)
                        self.assertEqual(error, expected_error)
                        self.assertIsNotNone(completed_at)
                    finally:
                        release.set()
                        task = executor.wait_for_task(task_id, timeout=5)
                    self.assertIsNotNone(task)
                    self.assertEqual(task["status"], terminal.value)
                    self.assertEqual(task["result"], expected_result)
                    self.assertEqual(task["error"], expected_error)
                    self.assertIsNotNone(task["completed_at"])
