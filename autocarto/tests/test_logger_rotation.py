import logging
import os
import tempfile
import unittest
from logging import LogRecord
from logging.handlers import RotatingFileHandler

import autocarto.utils.logger as logger_module


class LoggerRotationTestCase(unittest.TestCase):
    def test_configure_policy_uses_rotating_file_handler(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            logger_module.configure_log_policy(max_size=512, max_backups=1)
            logger_module.get_logger(__name__)
            logger_module.init_log_dir(tmpdir)

            root = logging.getLogger()
            file_handlers = [h for h in root.handlers if isinstance(h, RotatingFileHandler)]
            self.assertTrue(file_handlers, "expected rotating file handler to be installed")

            test_logger = logging.getLogger("autocarto.tests.logger_rotation")
            for _ in range(200):
                test_logger.debug("x" * 40)

            for handler in file_handlers:
                handler.flush()

            log_file = os.path.join(tmpdir, "autocarto.log")
            backup_file = os.path.join(tmpdir, "autocarto.log.1")

            self.assertTrue(os.path.exists(log_file))
            self.assertTrue(os.path.exists(backup_file))

    def test_file_handler_recreates_missing_directory(self):
        formatter = logging.Formatter('%(asctime)s [%(name)s] [%(levelname)s] %(message)s')
        with tempfile.TemporaryDirectory() as tmpdir:
            handler, actual_log_dir, err = logger_module._create_file_handler(
                tmpdir,
                'autocarto.log',
                logging.DEBUG,
                formatter,
            )
            self.assertIsNotNone(handler)
            self.assertEqual(actual_log_dir, tmpdir)
            self.assertIsNone(err)
            self.addCleanup(handler.close)

        record = LogRecord(
            name='autocarto.tests.logger_rotation',
            level=logging.INFO,
            pathname=__file__,
            lineno=1,
            msg='recreate directory',
            args=(),
            exc_info=None,
        )
        handler.emit(record)
        self.assertTrue(os.path.exists(os.path.join(tmpdir, 'autocarto.log')))
        handler.close()


if __name__ == "__main__":
    unittest.main()
