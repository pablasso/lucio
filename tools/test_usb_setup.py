import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("usb_setup", Path(__file__).with_name("usb-setup.py"))
usb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(usb)


class RedactionTests(unittest.TestCase):
    def test_multiline_command_echoes(self):
        text = "banner\nwifi_conf_set TestNetwork secret1 wpa2 0\nPSK: secret2\nwifi_psk_set secret3\nOK\n"
        redacted = usb.redact_text(text)
        for secret in ("secret1", "secret2", "secret3"):
            self.assertNotIn(secret, redacted)
        self.assertIn("OK", redacted)


if __name__ == "__main__":
    unittest.main()
