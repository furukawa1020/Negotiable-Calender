"""Guard the isolated, static-only launch site (no cloud credentials required)."""
import json
import re
from html.parser import HTMLParser
from pathlib import Path
import unittest
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[2]
APP = "https://negotiable-calendar-480760664246.asia-northeast1.run.app"
SITE = "negotiable-calendar-480760"


class Page(HTMLParser):
    def __init__(self, content):
        super().__init__(convert_charrefs=True)
        self.tags = []
        self.feed(content)

    def handle_starttag(self, tag, attrs):
        self.tags.append((tag, dict(attrs)))


class FirebaseHostingTest(unittest.TestCase):
    def setUp(self):
        self.config = json.loads((ROOT / "firebase.json").read_text())
        self.hosting = self.config["hosting"]
        self.public = ROOT / self.hosting["public"]

    def test_deployment_is_exactly_one_explicit_site(self):
        self.assertEqual(set(self.config), {"hosting"})
        self.assertEqual(self.hosting["site"], SITE)
        self.assertNotIn("target", self.hosting)
        self.assertNotIn("rewrites", self.hosting)
        self.assertNotIn("redirects", self.hosting)
        self.assertEqual(self.hosting["public"], "hosting/public")

    def test_public_files_are_allowlisted(self):
        paths = list(self.public.rglob("*"))
        self.assertTrue(all(not path.is_symlink() for path in paths))
        self.assertEqual(
            {p.relative_to(self.public).as_posix() for p in paths if p.is_file()},
            {"index.html", "styles.css", "404.html", "privacy.html", "terms.html"},
        )
        self.assertLess(sum(p.stat().st_size for p in paths if p.is_file()), 60000)

    def test_security_headers(self):
        rules = self.hosting["headers"]
        self.assertEqual(len(rules), 1)
        self.assertEqual(rules[0]["source"], "**")
        headers = {h["key"]: h["value"] for h in rules[0]["headers"]}
        csp = headers["Content-Security-Policy"]
        for directive in ("default-src 'none'", "style-src 'self'", "frame-ancestors 'none'", "form-action 'none'", "base-uri 'none'"):
            self.assertIn(directive, csp)
        self.assertNotIn("unsafe-inline", csp)
        self.assertEqual(headers["X-Content-Type-Options"], "nosniff")
        self.assertEqual(headers["X-Frame-Options"], "DENY")
        self.assertEqual(headers["Referrer-Policy"], "no-referrer")
        self.assertEqual(headers["Cache-Control"], "public, max-age=0, must-revalidate")

    def test_pages_have_only_local_assets_and_approved_links(self):
        for file in self.public.glob("*.html"):
            page = Page(file.read_text(encoding="utf-8"))
            ids = {attrs["id"] for _, attrs in page.tags if "id" in attrs}
            self.assertIn(("html", {"lang": "ja"}), page.tags)
            for tag, attrs in page.tags:
                self.assertNotIn(tag, {"script", "iframe", "form", "object", "embed", "base"})
                self.assertFalse(any(k.startswith("on") or k == "style" for k in attrs))
                self.assertNotIn("src", attrs)
                if "href" not in attrs:
                    continue
                href = attrs["href"]
                if tag == "link":
                    self.assertEqual(href, "/styles.css")
                elif href.startswith("#"):
                    self.assertIn(href[1:], ids)
                elif urlparse(href).scheme:
                    self.assertIn(href, {APP, "mailto:f.kotaro.0530@gmail.com",
                        "https://developers.google.com/terms/api-services-user-data-policy"})
                else:
                    target = urlparse(href)
                    self.assertIn(target.path, {"/", "/privacy.html", "/terms.html"})
                    destination = self.public / (target.path.lstrip("/") or "index.html")
                    self.assertTrue(destination.is_file())
                    if target.fragment:
                        destination_page = Page(destination.read_text(encoding="utf-8"))
                        self.assertIn(target.fragment, {a.get("id") for _, a in destination_page.tags})

    def test_honest_launch_status_and_app_destination(self):
        content = (self.public / "index.html").read_text(encoding="utf-8")
        self.assertIn(f'href="{APP}"', content)
        for disclosure in ("公開設定・審査が未完了", "現在拒否される場合", "表示イメージ", '/privacy.html', '/terms.html'):
            self.assertIn(disclosure, content)

    def test_policy_pages_are_readable_and_disclose_current_limits(self):
        for name in ("privacy.html", "terms.html"):
            content = (self.public / name).read_text(encoding="utf-8")
            page = Page(content)
            self.assertEqual(sum(tag == "h1" for tag, _ in page.tags), 1)
            self.assertIn(("main", {"id": "main", "class": "legal wrap"}), page.tags)
            for required in ("2026年9月23日", "はたけ/Furukawa", "f.kotaro.0530@gmail.com", "変更履歴"):
                self.assertIn(required, content)
        privacy = (self.public / "privacy.html").read_text(encoding="utf-8")
        for required in ("calendar.events.owned.readonly", "calendar.readonly", "Limited Use",
                         "端末内モデル", "新たな同意", "30日", "400日", "旧ユーザーID",
                         "自動削除期限は設定していません", "全データの即時完全消去は保証しません"):
            self.assertIn(required, privacy)
        terms = (self.public / "terms.html").read_text(encoding="utf-8")
        for required in ("自動課金は行いません", "申し込みなしに課金しません", "責任を排除するものではありません"):
            self.assertIn(required, terms)

    def test_shared_product_tokens_and_no_decorative_card_effects(self):
        app_css = (ROOT / "web/src/styles.css").read_text(encoding="utf-8")
        public_css = (self.public / "styles.css").read_text(encoding="utf-8")
        tokens = lambda css: dict(re.findall(r"(--[a-z-]+):\s*(#[0-9a-f]+);", css))
        app_tokens, public_tokens = tokens(app_css), tokens(public_css)
        for name in ("--ink", "--muted", "--line", "--paper", "--canvas", "--chrome",
                     "--green", "--green-soft", "--yellow", "--yellow-soft", "--red", "--red-soft"):
            self.assertIn(name, app_tokens)
            self.assertEqual(app_tokens[name], public_tokens[name])
        for css in (app_css, public_css):
            for effect in ("backdrop-filter", "radial-gradient", "linear-gradient", ".eyebrow", ".request-card", ".person-card"):
                self.assertNotIn(effect, css)


if __name__ == "__main__":
    unittest.main()
