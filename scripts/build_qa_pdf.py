"""Render the historical QA Markdown as a PDF; no screenshots are fabricated."""

from pathlib import Path
import html
import re

from reportlab.lib import colors
from reportlab.lib.enums import TA_LEFT
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle,
    PageBreak,
)

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "docs" / "qa" / "QA-2026-09-16.md"
OUTPUT = ROOT / "docs" / "qa" / "QA-roles-2026-09-16.pdf"
BRAND = colors.HexColor("#376E70")
INK = colors.HexColor("#172638")
MUTED = colors.HexColor("#596775")
PALE = colors.HexColor("#EFF6F5")


def inline(text):
    text = html.escape(text)
    text = re.sub(r"\*\*(.*?)\*\*", r"<b>\1</b>", text)
    text = re.sub(r"`([^`]+)`", r'<font name="Courier">\1</font>', text)
    return text


def make_pdf():
    # Embed installed TrueType fonts when available, avoiding renderer substitutes.
    regular = Path("/System/Library/Fonts/Supplemental/Arial.ttf")
    bold = Path("/System/Library/Fonts/Supplemental/Arial Bold.ttf")
    if regular.exists() and bold.exists():
        pdfmetrics.registerFont(TTFont("SSBody", str(regular)))
        pdfmetrics.registerFont(TTFont("SSBold", str(bold)))
        pdfmetrics.registerFontFamily("SSBody", normal="SSBody", bold="SSBold")
        body_font, bold_font = "SSBody", "SSBold"
    else:
        body_font, bold_font = "Helvetica", "Helvetica-Bold"
    styles = getSampleStyleSheet()
    styles.add(ParagraphStyle("ReportBody", fontName=body_font, fontSize=9.2,
                              leading=14, textColor=INK, spaceAfter=8))
    styles.add(ParagraphStyle("ReportH1", fontName=bold_font, fontSize=18,
                              leading=23, textColor=BRAND, spaceAfter=14))
    styles.add(ParagraphStyle("ReportH2", fontName=bold_font, fontSize=13,
                              leading=17, textColor=BRAND, spaceBefore=8, spaceAfter=10))
    styles.add(ParagraphStyle("Cell", fontName=body_font, fontSize=8.2,
                              leading=11.5, textColor=INK, alignment=TA_LEFT))
    styles.add(ParagraphStyle("HeadCell", fontName=bold_font, fontSize=8,
                              leading=11, textColor=colors.white))
    styles.add(ParagraphStyle("CoverTitle", fontName=bold_font, fontSize=29,
                              leading=35, textColor=INK, spaceAfter=18))
    styles.add(ParagraphStyle("CoverLabel", fontName=bold_font, fontSize=10,
                              leading=14, textColor=BRAND, spaceAfter=10))
    styles.add(ParagraphStyle("CoverSub", fontName=body_font, fontSize=12,
                              leading=19, textColor=MUTED, spaceAfter=14))
    doc = SimpleDocTemplate(str(OUTPUT), pagesize=A4, rightMargin=18*mm,
                            leftMargin=18*mm, topMargin=23*mm, bottomMargin=21*mm,
                            title="QA Historis Standard Setting - 16 September 2026",
                            author="Standard Setting", subject="Hasil pengujian Admin, PIC dan Panelis")
    story = [Spacer(1, 20*mm),
             Paragraph("STANDARD SETTING / QUALITY ASSURANCE", styles["CoverLabel"]),
             Paragraph("Laporan QA<br/>Historis Per Role", styles["CoverTitle"]),
             Paragraph("Admin Super, PIC Mapel, dan Panelis", styles["CoverSub"]),
             Spacer(1, 9*mm)]
    summary = [
        ["Tanggal pengujian", "16 September 2026 - Asia/Jakarta"],
        ["Tanggal penyusunan", "12 Oktober 2026"],
        ["Lingkup", "28 kasus manual dan pemeriksaan API/source"],
        ["Kesimpulan", "Belum lulus keseluruhan"],
    ]
    summary_table = Table([[Paragraph(a, styles["Cell"]), Paragraph(b, styles["Cell"])]
                           for a, b in summary], colWidths=[45*mm, 126*mm])
    summary_table.setStyle(TableStyle([
        ("BACKGROUND", (0, 0), (-1, -1), PALE),
        ("VALIGN", (0, 0), (-1, -1), "TOP"),
        ("LEFTPADDING", (0, 0), (-1, -1), 12),
        ("RIGHTPADDING", (0, 0), (-1, -1), 12),
        ("TOPPADDING", (0, 0), (-1, -1), 12),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 12),
    ]))
    story += [summary_table, Spacer(1, 12*mm),
              Paragraph("Dokumen ini menyusun kembali catatan QA terdahulu. "
                        "Tidak dilakukan retest browser pada tanggal penyusunan. "
                        "Data demo dan alur yang belum teruji tidak dianggap hasil lulus.", styles["ReportBody"]),
              Paragraph("Bukti screenshot pernah terlihat dalam percakapan, tetapi tidak tersedia "
                        "sebagai berkas lokal untuk dilampirkan. Laporan menggunakan catatan "
                        "observasi yang tersedia dan tidak membuat ulang screenshot.", styles["ReportBody"]),
              PageBreak()]

    lines = SOURCE.read_text(encoding="utf-8").splitlines()
    # The first section starts the body; metadata is already represented on cover.
    start = next(i for i, line in enumerate(lines) if line.startswith("## 1."))
    i = start
    while i < len(lines):
        line = lines[i].strip()
        if not line:
            i += 1
            continue
        if line.startswith("## "):
            if i != start:
                story.append(PageBreak())
            story.append(Paragraph(inline(line[3:]), styles["ReportH1"]))
            i += 1
        elif line.startswith("| "):
            raw_rows = []
            while i < len(lines) and lines[i].strip().startswith("|"):
                cells = [c.strip() for c in lines[i].strip().strip("|").split("|")]
                if not all(re.match(r"^:?-+:?$", c.replace(" ", "")) for c in cells):
                    raw_rows.append(cells)
                i += 1
            ncols = len(raw_rows[0])
            widths = [32*mm, 139*mm] if ncols == 2 else [17*mm, 53*mm, 73*mm, 28*mm] if ncols == 4 else [40*mm, 23*mm, 108*mm]
            rows = [[Paragraph(inline(c), styles["HeadCell" if r == 0 else "Cell"])
                     for c in cells] for r, cells in enumerate(raw_rows)]
            table = Table(rows, colWidths=widths, repeatRows=1, hAlign="LEFT")
            table.setStyle(TableStyle([
                ("BACKGROUND", (0, 0), (-1, 0), BRAND),
                ("ROWBACKGROUNDS", (0, 1), (-1, -1), [colors.white, PALE]),
                ("VALIGN", (0, 0), (-1, -1), "TOP"),
                ("LEFTPADDING", (0, 0), (-1, -1), 7),
                ("RIGHTPADDING", (0, 0), (-1, -1), 7),
                ("TOPPADDING", (0, 0), (-1, -1), 7),
                ("BOTTOMPADDING", (0, 0), (-1, -1), 7),
                ("LINEBELOW", (0, 0), (-1, 0), 0.5, BRAND),
                ("LINEBELOW", (0, 1), (-1, -1), 0.25, colors.HexColor("#DDE5E8")),
            ]))
            story += [table, Spacer(1, 10)]
        else:
            paragraph = [line]
            i += 1
            while i < len(lines) and lines[i].strip() and not lines[i].startswith(("## ", "| ")):
                # Numbered list entries are individual paragraphs.
                if re.match(r"^\d+\. ", lines[i].strip()):
                    break
                paragraph.append(lines[i].strip())
                i += 1
            story.append(Paragraph(inline(" ".join(paragraph)), styles["ReportBody"]))

    def footer(canvas, document):
        width, height = A4
        canvas.saveState()
        canvas.setStrokeColor(colors.HexColor("#DDE5E8"))
        canvas.line(18*mm, height-15*mm, width-18*mm, height-15*mm)
        canvas.setFont(bold_font, 8)
        canvas.setFillColor(BRAND)
        canvas.drawString(18*mm, height-11*mm, "STANDARD SETTING")
        canvas.setFont(body_font, 8)
        canvas.setFillColor(MUTED)
        canvas.drawRightString(width-18*mm, height-11*mm, "QA HISTORIS / 16 SEP 2026")
        canvas.drawString(18*mm, 12*mm, "Disusun 12 Okt 2026 - Tidak menyatakan hasil retest")
        canvas.drawRightString(width-18*mm, 12*mm, str(document.page))
        canvas.restoreState()

    doc.build(story, onFirstPage=footer, onLaterPages=footer)
    print(OUTPUT)


if __name__ == "__main__":
    make_pdf()
