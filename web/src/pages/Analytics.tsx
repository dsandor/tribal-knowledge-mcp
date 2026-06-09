import { useEffect, useState } from 'react';
import { fetchUsage, fetchGaps, fetchContributions } from '../lib/api';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Paper from '@mui/material/Paper';
import Table from '@mui/material/Table';
import TableHead from '@mui/material/TableHead';
import TableBody from '@mui/material/TableBody';
import TableRow from '@mui/material/TableRow';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import LinearProgress from '@mui/material/LinearProgress';
import Chip from '@mui/material/Chip';

interface TopEntry { id: string; title: string; domain: string; score: number; usage_count: number; rating: number; }
interface DomainStat { domain: string; entry_count: number; avg_rating: number; total_usage: number; }
interface HeatPoint { week: string; domain: string; usage: number; }
interface Gap { domain: string; entry_count: number; threshold: number; severity: 'low' | 'medium' | 'high'; }
interface LeaderEntry { author: string; entry_count: number; approved_count: number; total_usage: number; avg_rating: number; score: number; }

const severityChipColor: Record<string, 'warning' | 'error'> = {
  low: 'warning',
  medium: 'error',
  high: 'error',
};

export default function Analytics() {
  const [topEntries, setTopEntries] = useState<TopEntry[]>([]);
  const [domainStats, setDomainStats] = useState<DomainStat[]>([]);
  const [heatmap, setHeatmap] = useState<HeatPoint[]>([]);
  const [gaps, setGaps] = useState<Gap[]>([]);
  const [leaderboard, setLeaderboard] = useState<LeaderEntry[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([fetchUsage(), fetchGaps(), fetchContributions()]).then(([usage, gapsData, contribs]) => {
      setTopEntries(usage.top_entries ?? []);
      setDomainStats(usage.by_domain ?? []);
      setHeatmap(usage.heatmap ?? []);
      setGaps(gapsData.gaps ?? []);
      setLeaderboard(contribs.leaderboard ?? []);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <Box sx={{ p: 6, display: 'flex', alignItems: 'center', gap: 2 }}>
        <CircularProgress size={20} />
        <Typography color="text.secondary">Loading analytics...</Typography>
      </Box>
    );
  }

  const weeks = [...new Set(heatmap.map(h => h.week))].sort().reverse().slice(0, 12);
  const domains = [...new Set(heatmap.map(h => h.domain))].filter(Boolean);
  const heatIndex = new Map(heatmap.map(h => [`${h.week}:${h.domain}`, h.usage]));
  const maxUsage = Math.max(1, ...heatmap.map(h => h.usage));

  return (
    <Box sx={{ p: 3, maxWidth: '72rem' }}>
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 4 }}>Analytics</Typography>

      {weeks.length > 0 && domains.length > 0 && (
        <Box sx={{ mb: 5 }}>
          <Typography variant="h6" gutterBottom>Usage Heatmap</Typography>
          <Box sx={{ overflowX: 'auto' }}>
            <table style={{ borderCollapse: 'collapse', fontSize: 12 }}>
              <thead>
                <tr>
                  <th style={{ paddingRight: 12, textAlign: 'right' }}>
                    <Typography variant="caption" color="text.secondary">Domain</Typography>
                  </th>
                  {weeks.map(w => (
                    <th key={w} style={{ padding: '0 4px' }}>
                      <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 400 }}>{w}</Typography>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {domains.map(domain => (
                  <tr key={domain}>
                    <td style={{ paddingRight: 12, textAlign: 'right' }}>
                      <Typography variant="caption" color="text.secondary">{domain}</Typography>
                    </td>
                    {weeks.map(week => {
                      const count = heatIndex.get(`${week}:${domain}`) ?? 0;
                      return (
                        <td key={week} style={{ padding: '0 4px' }}>
                          <Box
                            sx={{ width: 20, height: 20, borderRadius: '2px' }}
                            style={{
                              backgroundColor: count > 0
                                ? `rgba(99,102,241,${count / maxUsage})`
                                : 'rgba(255,255,255,0.05)',
                            }}
                            title={`${domain} / ${week}: ${count}`}
                          />
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </Box>
        </Box>
      )}

      {domainStats.length > 0 && (
        <Box sx={{ mb: 5 }}>
          <Typography variant="h6" gutterBottom>By Domain</Typography>
          <TableContainer component={Paper}>
            <Table size="small">
              <TableHead>
                <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)' } }}>
                  <TableCell>Domain</TableCell>
                  <TableCell align="right">Entries</TableCell>
                  <TableCell align="right">Avg Rating</TableCell>
                  <TableCell align="right">Total Usage</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {domainStats.map(d => (
                  <TableRow key={d.domain} hover>
                    <TableCell>{d.domain}</TableCell>
                    <TableCell align="right">{d.entry_count}</TableCell>
                    <TableCell align="right">{d.avg_rating.toFixed(1)}</TableCell>
                    <TableCell align="right">{d.total_usage}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </Box>
      )}

      <Box sx={{ mb: 5 }}>
        <Typography variant="h6" gutterBottom>Top Entries</Typography>
        <TableContainer component={Paper}>
          <Table size="small">
            <TableHead>
              <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)' } }}>
                <TableCell>Title</TableCell>
                <TableCell>Domain</TableCell>
                <TableCell align="right">Rating</TableCell>
                <TableCell align="right">Usage</TableCell>
                <TableCell align="right">Score</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {topEntries.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} align="center">
                    <Typography color="text.secondary" variant="body2" sx={{ py: 1 }}>No entries yet.</Typography>
                  </TableCell>
                </TableRow>
              ) : topEntries.map(e => (
                <TableRow key={e.id} hover>
                  <TableCell>{e.title}</TableCell>
                  <TableCell>
                    <Typography variant="body2" color="text.secondary">{e.domain}</Typography>
                  </TableCell>
                  <TableCell align="right">{e.rating.toFixed(1)}</TableCell>
                  <TableCell align="right">{e.usage_count}</TableCell>
                  <TableCell align="right" sx={{ fontFamily: 'monospace' }}>{e.score.toFixed(1)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      </Box>

      <Box sx={{ mb: 5 }}>
        <Typography variant="h6" gutterBottom>Coverage Gaps</Typography>
        {gaps.length === 0 ? (
          <Typography variant="body2" color="text.secondary">No gaps detected.</Typography>
        ) : (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
            {gaps.map(g => (
              <Paper key={g.domain} sx={{ p: 2, display: 'flex', alignItems: 'center', gap: 2 }}>
                <Chip
                  label={g.severity.toUpperCase()}
                  color={severityChipColor[g.severity] ?? 'default'}
                  size="small"
                  sx={g.severity === 'medium' ? { bgcolor: '#ea580c', color: '#fff' } : {}}
                />
                <Typography sx={{ fontWeight: 500, minWidth: 120 }}>{g.domain}</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ minWidth: 100 }}>
                  {g.entry_count} / {g.threshold} entries
                </Typography>
                <Box sx={{ flex: 1 }}>
                  <LinearProgress
                    variant="determinate"
                    value={Math.min(100, (g.entry_count / g.threshold) * 100)}
                    sx={{ height: 8, borderRadius: 4 }}
                  />
                </Box>
              </Paper>
            ))}
          </Box>
        )}
      </Box>

      <Box sx={{ mb: 5 }}>
        <Typography variant="h6" gutterBottom>Contribution Leaderboard</Typography>
        <TableContainer component={Paper}>
          <Table size="small">
            <TableHead>
              <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)' } }}>
                <TableCell>#</TableCell>
                <TableCell>Author</TableCell>
                <TableCell align="right">Entries</TableCell>
                <TableCell align="right">Approved</TableCell>
                <TableCell align="right">Avg Rating</TableCell>
                <TableCell align="right">Total Usage</TableCell>
                <TableCell align="right">Score</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {leaderboard.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} align="center">
                    <Typography color="text.secondary" variant="body2" sx={{ py: 1 }}>No contributions yet.</Typography>
                  </TableCell>
                </TableRow>
              ) : leaderboard.map((l, i) => (
                <TableRow key={l.author} hover>
                  <TableCell>
                    <Typography variant="body2" color="text.secondary">{i + 1}</Typography>
                  </TableCell>
                  <TableCell sx={{ fontWeight: 500 }}>{l.author}</TableCell>
                  <TableCell align="right">{l.entry_count}</TableCell>
                  <TableCell align="right">{l.approved_count}</TableCell>
                  <TableCell align="right">{l.avg_rating.toFixed(1)}</TableCell>
                  <TableCell align="right">{l.total_usage}</TableCell>
                  <TableCell align="right" sx={{ fontFamily: 'monospace' }}>{l.score.toFixed(1)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      </Box>
    </Box>
  );
}
