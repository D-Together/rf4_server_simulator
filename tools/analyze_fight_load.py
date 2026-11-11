#!/usr/bin/env python3
"""Find the 14/8 telemetry slot that carries a decreasing fish value.

Reads a trace file produced by tools/capture_fight_load.ps1, extracts the raw
hex of each 'TRACE recv cmd=14/8' frame, then scans every 4-byte little-endian
window across frames for values that decrease while the fight progresses.

This is deliberately byte-level: the 14/8 field layout is exactly what we are
trying to discover, so no protocol definitions are assumed. A candidate is a
hypothesis, not proof -- confirm it against reel damage log load_fields.

Usage:
    python tools/analyze_fight_load.py logs/_fight_load_trace.txt
    python tools/analyze_fight_load.py logs/_fight_load_trace.txt --values 12
    python tools/analyze_fight_load.py --selftest
"""

import argparse
import math
import re
import struct
import sys

HEX_RE = re.compile(r'"?hex"?\s*[=:]\s*"?([0-9a-fA-F]{8,})"?([^\s]*)')
CMD_RE = re.compile(r'14\s*/\s*8')
KINDS = ('f32', 'i32', 'u32')


def extract_frames(path):
    """Return (frames, truncated_count). Silently skips lines without hex."""
    frames = []
    truncated = 0
    with open(path, 'r', encoding='utf-8', errors='replace') as handle:
        for line in handle:
            if not CMD_RE.search(line):
                continue
            match = HEX_RE.search(line)
            if not match:
                continue
            raw_hex, tail = match.group(1), match.group(2)
            if '…' in tail or '+' in tail or tail.startswith('...'):
                truncated += 1
            if len(raw_hex) % 2:
                raw_hex = raw_hex[:-1]
            try:
                frames.append(bytes.fromhex(raw_hex))
            except ValueError:
                continue
    return frames, truncated


def plausible_float(value):
    return math.isfinite(value) and 1e-4 <= abs(value) <= 1e7


def decode_at(raw, offset, kind):
    chunk = raw[offset:offset + 4]
    if kind == 'f32':
        value = struct.unpack('<f', chunk)[0]
        return float(value) if plausible_float(value) else None
    if kind == 'i32':
        value = struct.unpack('<i', chunk)[0]
        return float(value) if abs(value) <= 1e7 else None
    value = struct.unpack('<I', chunk)[0]
    return float(value) if value <= 1e7 else None


def series(frames, offset, kind, common_len):
    """Values for one (offset, kind) across all frames, or None if unusable."""
    values = []
    for raw in frames:
        if offset + 4 > common_len:
            return None
        value = decode_at(raw, offset, kind)
        if value is None:
            return None
        values.append(value)
    if len(values) < 3:
        return None
    return values


def stats(values):
    pairs = list(zip(values, values[1:]))
    changes = [(a, b) for a, b in pairs if a != b]
    if not changes or not pairs:
        return 0.0, 0.0, 0.0, 0.0, 0.0, 0.0
    down = sum(1 for a, b in changes if b < a)
    down_ratio = down / len(changes)
    up_ratio = 1.0 - down_ratio
    delta = values[-1] - values[0]
    # Coverage counts a decrease (or increase) against ALL adjacent steps.
    # Without it, a single state flip on the last frame scores 1.00 and looks
    # exactly like a continuously draining stamina bar.
    down_cov = down / len(pairs)
    up_cov = (len(changes) - down) / len(pairs)
    # A state flag that flips only on the landing frame puts almost the whole
    # delta in the last step; a draining bar spreads it across many steps.
    last_share = 0.0
    if len(values) >= 2 and delta != 0:
        last_share = abs(values[-1] - values[-2]) / abs(delta)
    return down_ratio, up_ratio, delta, down_cov, up_cov, last_share


def analyze(frames, min_down=0.70, min_coverage=0.50, max_last_share=0.80):
    """Return (candidates, increasing, common_len, unequal_lengths)."""
    if len(frames) < 3:
        return [], [], 0, False
    lengths = [len(f) for f in frames]
    common_len = min(lengths)
    unequal = len(set(lengths)) > 1
    candidates = []
    increasing = []
    for offset in range(0, common_len - 3):
        for kind in KINDS:
            values = series(frames, offset, kind, common_len)
            if values is None:
                continue
            down_ratio, up_ratio, delta, down_cov, up_cov, last_share = stats(values)
            row = {
                'offset': offset, 'kind': kind, 'values': values,
                'first': values[0], 'last': values[-1],
                'delta': delta, 'down': down_ratio, 'up': up_ratio,
                'down_cov': down_cov, 'up_cov': up_cov, 'last_share': last_share,
            }
            spread = last_share <= max_last_share
            if delta < 0 and down_ratio >= min_down and down_cov >= min_coverage and spread:
                candidates.append(row)
            if delta > 0 and up_ratio >= min_down and up_cov >= min_coverage and spread:
                increasing.append(row)
    float_offsets = {r['offset'] for r in candidates + increasing if r['kind'] == 'f32'}
    for row in candidates + increasing:
        # A 4-byte window shifted 1-3 bytes over a float is still read as an
        # integer; those are byte-overlap artifacts, not independent fields.
        row['artifact'] = row['kind'] != 'f32' and any(
            abs(row['offset'] - other) <= 3 for other in float_offsets)

    def rank(row, ratio_key):
        relative = abs(row['delta']) / max(abs(row['first']), 1e-9)
        return (row['artifact'], -row[ratio_key], -relative, row['offset'])

    candidates.sort(key=lambda r: rank(r, 'down'))
    increasing.sort(key=lambda r: rank(r, 'up'))
    return candidates, increasing, common_len, unequal


def print_table(title, rows, limit):
    print()
    print(title)
    if not rows:
        print('  (none)')
        return
    print('  off  kind   first        last         delta       ratio  cov   n    note')
    for row in rows[:limit]:
        ratio = row['down'] if row['delta'] < 0 else row['up']
        cov = row['down_cov'] if row['delta'] < 0 else row['up_cov']
        note = 'overlap?' if row.get('artifact') else ''
        print('  {:3d}  {:<4}  {:11.4f}  {:11.4f}  {:11.4f}  {:5.2f}  {:4.2f}  {:2d}  {}'.format(
            row['offset'], row['kind'], row['first'], row['last'],
            row['delta'], ratio, cov, len(row['values']), note))


def selftest():
    """Hand-built frames: known decreasing float at 8, increasing counter at 16."""
    frames = []
    expected = [100.0, 82.0, 64.0, 46.0, 28.0, 10.0]
    for index, value in enumerate(expected):
        raw = bytearray(32)
        struct.pack_into('<f', raw, 8, value)
        struct.pack_into('<I', raw, 16, 1000 + index)
        frames.append(bytes(raw))
    candidates, increasing, _, _ = analyze(frames, min_down=0.70)
    hit = [r for r in candidates if r['offset'] == 8 and r['kind'] == 'f32']
    assert hit, 'selftest: decreasing float slot at offset 8 not found'
    assert hit[0]['down'] == 1.0, 'selftest: expected strictly decreasing'
    assert abs(hit[0]['first'] - 100.0) < 1e-6 and abs(hit[0]['last'] - 10.0) < 1e-6, \
        'selftest: endpoint values do not match hand-built literals'
    assert candidates[0]['offset'] == 8 and candidates[0]['kind'] == 'f32', \
        'selftest: byte-overlap integer artifacts outranked the real float slot'
    ctrl = [r for r in increasing if r['offset'] == 16 and r['delta'] > 0]
    assert ctrl, 'selftest: increasing control counter at offset 16 not found'
    flip_frames = []
    for index in range(6):
        raw = bytearray(32)
        struct.pack_into('<f', raw, 8, 2.0 if index < 5 else 0.6)
        flip_frames.append(bytes(raw))
    flip_candidates, _, _, _ = analyze(flip_frames, min_down=0.70)
    assert not any(r['offset'] == 8 and r['kind'] == 'f32' for r in flip_candidates), \
        'selftest: a single end-frame state flip must not count as a drain'
    print('selftest OK: f32@8 drain 100.0 -> 10.0 found; u32/i32@16 control increasing; '
          'end-frame state flip rejected')
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('path', nargs='?', default='logs/_fight_load_trace.txt',
                        help='trace file from capture_fight_load.ps1')
    parser.add_argument('--min-down', type=float, default=0.70,
                        help='minimum monotone ratio among changed steps (default 0.70)')
    parser.add_argument('--min-coverage', type=float, default=0.50,
                        help='minimum fraction of ALL steps that must decrease (default 0.50)')
    parser.add_argument('--max-last-share', type=float, default=0.80,
                        help='reject if this much of the delta happens in the final step (default 0.80)')
    parser.add_argument('--top', type=int, default=12,
                        help='how many candidates to print (default 12)')
    parser.add_argument('--values', type=int, default=None,
                        help='print per-frame values for this byte offset')
    parser.add_argument('--selftest', action='store_true',
                        help='run built-in synthetic-frame check and exit')
    args = parser.parse_args()

    if args.selftest:
        return selftest()

    try:
        frames, truncated = extract_frames(args.path)
    except OSError as exc:
        print('cannot read trace file: {}'.format(exc), file=sys.stderr)
        return 2

    lengths = [len(f) for f in frames]
    print('trace: {}'.format(args.path))
    print('14/8 frames: {}'.format(len(frames)))
    if lengths:
        print('hex bytes: min={} max={}'.format(min(lengths), max(lengths)))
    if truncated:
        print('WARNING: {} frame(s) look truncated. Re-run capture with '
              'data/trace_full.on present.'.format(truncated))
    if len(frames) < 3:
        print('need at least 3 frames; capture a longer fight.', file=sys.stderr)
        return 1

    candidates, increasing, common_len, unequal = analyze(
        frames, args.min_down, args.min_coverage, args.max_last_share)
    if unequal:
        print('WARNING: frame lengths differ; offsets may drift. '
              'Using common prefix of {} bytes.'.format(common_len))
    print_table('candidate DECREASING slots (ratio >= {:.2f}):'.format(args.min_down),
                candidates, args.top)
    print_table('candidate INCREASING slots (control / elapsed time):',
                increasing, min(args.top, 6))

    if args.values is not None:
        print()
        print('per-frame values at offset {}:'.format(args.values))
        for kind in KINDS:
            values = series(frames, args.values, kind, common_len)
            if values is None:
                continue
            print('  {}: {}'.format(kind, '  '.join('{:.4f}'.format(v) for v in values)))

    print()
    print('REMINDER: a candidate is only a hypothesis. Cross-check its first/last')
    print('against load_fields in the reel damage log, and confirm with a real')
    print('client fight before wiring it into any report or settlement path.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
