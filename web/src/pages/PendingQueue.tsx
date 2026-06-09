import { useEffect, useState } from 'react';
import { fetchPending, approveEntry, rejectEntry, batchApprove, batchReject } from '../lib/api';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Button from '@mui/material/Button';
import Checkbox from '@mui/material/Checkbox';
import Chip from '@mui/material/Chip';
import Paper from '@mui/material/Paper';
import Divider from '@mui/material/Divider';

interface Entry {
  id: string;
  title: string;
  content: string;
  domain: string;
  author: string;
  type: string;
  // Storage layer uses uppercase field names
  ID?: string;
  Title?: string;
  Content?: string;
  Domain?: string;
  Author?: string;
  Type?: string;
}

// Normalise entries from either camelCase or PascalCase shapes the server may return.
function normalise(raw: Record<string, unknown>): Entry {
  return {
    id: (raw.id ?? raw.ID ?? '') as string,
    title: (raw.title ?? raw.Title ?? '') as string,
    content: (raw.content ?? raw.Content ?? '') as string,
    domain: (raw.domain ?? raw.Domain ?? '') as string,
    author: (raw.author ?? raw.Author ?? '') as string,
    type: (raw.type ?? raw.Type ?? '') as string,
  };
}

export default function PendingQueue() {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkWorking, setBulkWorking] = useState(false);

  const load = () => {
    setLoading(true);
    fetchPending()
      .then((data: unknown) => {
        const arr = Array.isArray(data) ? data : [];
        setEntries(arr.map((r) => normalise(r as Record<string, unknown>)));
        setSelected(new Set());
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  // Single-entry actions
  const handleApprove = async (id: string) => {
    await approveEntry(id);
    setEntries((prev) => prev.filter((e) => e.id !== id));
    setSelected((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  };

  const handleReject = async (id: string) => {
    await rejectEntry(id);
    setEntries((prev) => prev.filter((e) => e.id !== id));
    setSelected((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  };

  // Multi-select helpers
  const allSelected = entries.length > 0 && selected.size === entries.length;

  const toggleSelectAll = () => {
    if (allSelected) {
      setSelected(new Set());
    } else {
      setSelected(new Set(entries.map((e) => e.id)));
    }
  };

  const toggleOne = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  // Bulk actions
  const handleBulkApprove = async () => {
    if (selected.size === 0) return;
    setBulkWorking(true);
    try {
      const ids = Array.from(selected);
      await batchApprove(ids);
      setEntries((prev) => prev.filter((e) => !selected.has(e.id)));
      setSelected(new Set());
    } finally {
      setBulkWorking(false);
    }
  };

  const handleBulkReject = async () => {
    if (selected.size === 0) return;
    setBulkWorking(true);
    try {
      const ids = Array.from(selected);
      await batchReject(ids);
      setEntries((prev) => prev.filter((e) => !selected.has(e.id)));
      setSelected(new Set());
    } finally {
      setBulkWorking(false);
    }
  };

  return (
    <Box sx={{ p: 3, maxWidth: '56rem' }}>
      {/* Page heading */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 3 }}>
        <Typography variant="h5" sx={{ fontWeight: 700 }}>Pending Queue</Typography>
        {!loading && (
          <Chip
            label={`${entries.length} ${entries.length === 1 ? 'entry' : 'entries'}`}
            size="small"
          />
        )}
      </Box>

      {/* Loading state */}
      {loading && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, py: 4 }}>
          <CircularProgress size={20} />
          <Typography color="text.secondary">Loading...</Typography>
        </Box>
      )}

      {/* Empty state */}
      {!loading && entries.length === 0 && (
        <Typography color="text.secondary" align="center" sx={{ py: 8 }}>
          No entries awaiting approval — the queue is clear.
        </Typography>
      )}

      {/* Content */}
      {!loading && entries.length > 0 && (
        <>
          {/* Header row: select-all + bulk actions */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2, pb: 1.5, borderBottom: '1px solid', borderColor: 'divider' }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, cursor: 'pointer' }} onClick={toggleSelectAll}>
              <Checkbox
                checked={allSelected}
                onChange={toggleSelectAll}
                size="small"
                sx={{ p: 0 }}
              />
              <Typography variant="body2" color="text.secondary">Select all</Typography>
            </Box>

            {selected.size > 0 && (
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, ml: 'auto' }}>
                <Typography variant="body2" color="text.secondary">{selected.size} selected</Typography>
                <Button
                  variant="contained"
                  color="success"
                  size="small"
                  disabled={bulkWorking}
                  onClick={handleBulkApprove}
                >
                  Approve selected
                </Button>
                <Button
                  variant="contained"
                  color="error"
                  size="small"
                  disabled={bulkWorking}
                  onClick={handleBulkReject}
                >
                  Reject selected
                </Button>
              </Box>
            )}
          </Box>

          {/* Entry cards */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
            {entries.map((e) => (
              <Paper
                key={e.id}
                sx={{
                  p: 2,
                  border: '1px solid',
                  borderColor: selected.has(e.id) ? 'rgba(255,255,255,0.2)' : 'divider',
                  bgcolor: selected.has(e.id) ? 'rgba(255,255,255,0.04)' : 'background.paper',
                  transition: 'border-color 0.15s, background-color 0.15s',
                }}
              >
                <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5 }}>
                  {/* Checkbox */}
                  <Checkbox
                    checked={selected.has(e.id)}
                    onChange={() => toggleOne(e.id)}
                    size="small"
                    sx={{ mt: 0.25, p: 0, flexShrink: 0 }}
                  />

                  {/* Main content */}
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Box sx={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 2 }}>
                      <Box sx={{ minWidth: 0 }}>
                        <Typography sx={{ fontWeight: 600 }} noWrap>{e.title}</Typography>
                        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, mt: 0.5, alignItems: 'center' }}>
                          {e.type && (
                            <Chip
                              label={e.type}
                              size="small"
                              variant="outlined"
                              sx={{ fontSize: 11 }}
                            />
                          )}
                          {e.domain && (
                            <Chip
                              label={e.domain}
                              size="small"
                              sx={{ fontSize: 11 }}
                            />
                          )}
                          {e.author && (
                            <Typography variant="caption" color="text.secondary">by {e.author}</Typography>
                          )}
                        </Box>
                      </Box>

                      {/* Per-card action buttons */}
                      <Box sx={{ display: 'flex', gap: 1, flexShrink: 0 }}>
                        <Button
                          variant="contained"
                          color="success"
                          size="small"
                          onClick={() => handleApprove(e.id)}
                        >
                          Approve
                        </Button>
                        <Button
                          variant="contained"
                          color="error"
                          size="small"
                          onClick={() => handleReject(e.id)}
                        >
                          Reject
                        </Button>
                      </Box>
                    </Box>

                    {/* Content preview */}
                    <Typography
                      variant="body2"
                      color="text.secondary"
                      sx={{
                        mt: 1,
                        display: '-webkit-box',
                        WebkitLineClamp: 3,
                        WebkitBoxOrient: 'vertical',
                        overflow: 'hidden',
                      }}
                    >
                      {e.content}
                    </Typography>
                  </Box>
                </Box>
              </Paper>
            ))}
          </Box>
        </>
      )}
    </Box>
  );
}
