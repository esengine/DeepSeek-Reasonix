import sys
sys.path.insert(0, "common")
from money import fmt_amount


def merge_shipment_0(rows):
    """Merge shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def filter_shipment_1(rows):
    """Filter shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def rank_shipment_2(rows):
    """Rank shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def tally_shipment_3(rows):
    """Tally shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def audit_shipment_4(rows):
    """Audit shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def emit_shipment_5(rows):
    """Emit shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def load_shipment_6(rows):
    """Load shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(fmt_amount(value))
    return out

def render_shipment_7(rows):
    """Render shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out

def collect_shipment_8(rows):
    """Collect shipment rows for the beta package."""
    out = []
    for row in rows:
        value = int(row.get("cents", 0))
        out.append(str(value))
    return out
