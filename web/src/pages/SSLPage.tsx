import { useState, useEffect } from 'react'
import { Shield, Plus, RefreshCw, Trash2, Check, X, Clock, AlertCircle } from 'lucide-react'
import { Layout } from '@/components/Layout'
import api from '@/lib/api'
import type { SSLCertificate, SSLCertificateRequest } from '@/lib/api'

export default function SSLPage() {
    const [certificates, setCertificates] = useState<SSLCertificate[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [showNewModal, setShowNewModal] = useState(false)

    useEffect(() => {
        loadCertificates()
    }, [])

    const loadCertificates = async () => {
        try {
            setLoading(true)
            const data = await api.getCertificates()
            setCertificates(data.certificates || [])
        } catch (err: any) {
            setError(err.message)
        } finally {
            setLoading(false)
        }
    }

    const handleRenew = async (id: string) => {
        try {
            await api.renewCertificate(id)
            await loadCertificates()
        } catch (err: any) {
            alert(`Failed to renew certificate: ${err.message}`)
        }
    }

    const handleDelete = async (id: string, domain: string) => {
        if (!confirm(`Delete certificate for ${domain}?`)) return

        try {
            await api.deleteCertificate(id)
            await loadCertificates()
        } catch (err: any) {
            alert(`Failed to delete certificate: ${err.message}`)
        }
    }

    const getStatusIcon = (status: string) => {
        switch (status) {
            case 'active':
                return <Check className="h-4 w-4 text-green-500" />
            case 'pending':
                return <Clock className="h-4 w-4 text-yellow-500" />
            case 'expired':
            case 'failed':
                return <X className="h-4 w-4 text-red-500" />
            default:
                return <AlertCircle className="h-4 w-4 text-gray-500" />
        }
    }

    const getTypeLabel = (type: string) => {
        switch (type) {
            case 'letsencrypt':
                return "Let's Encrypt"
            case 'custom':
                return 'Custom'
            case 'self-signed':
                return 'Self-Signed'
            default:
                return type
        }
    }

    const formatDate = (dateString: string) => {
        return new Date(dateString).toLocaleDateString('en-US', {
            year: 'numeric',
            month: 'short',
            day: 'numeric'
        })
    }

    const getDaysUntilExpiry = (validUntil: string) => {
        const days = Math.floor((new Date(validUntil).getTime() - Date.now()) / (1000 * 60 * 60 * 24))
        return days
    }

    const getExpiryColor = (days: number) => {
        if (days < 0) return 'text-red-500'
        if (days < 30) return 'text-yellow-500'
        return 'text-green-500'
    }

    return (
        <Layout>
            <div className="p-8">
                <div className="flex justify-between items-center mb-6">
                    <div>
                        <h1 className="text-2xl font-bold flex items-center gap-2">
                            <Shield className="h-7 w-7 text-green-500" />
                            SSL Certificates
                        </h1>
                        <p className="text-gray-400 mt-1">
                            Manage SSL/TLS certificates for your sites
                        </p>
                    </div>
                    <button
                        onClick={() => setShowNewModal(true)}
                        className="flex items-center gap-2 px-4 py-2 bg-green-600 hover:bg-green-700 rounded-lg font-medium transition-colors"
                    >
                        <Plus className="h-4 w-4" />
                        New Certificate
                    </button>
                </div>

                {error && (
                    <div className="mb-6 p-4 bg-red-500/10 border border-red-500/20 rounded-lg text-red-500">
                        {error}
                    </div>
                )}

                {loading ? (
                    <div className="text-center py-12">
                        <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-green-500"></div>
                    </div>
                ) : certificates.length === 0 ? (
                    <div className="text-center py-12 bg-gray-800/50 rounded-lg border border-gray-700">
                        <Shield className="h-12 w-12 text-gray-600 mx-auto mb-4" />
                        <h3 className="text-lg font-medium mb-2">No SSL Certificates</h3>
                        <p className="text-gray-400 mb-4">
                            Get started by issuing a certificate for your site
                        </p>
                        <button
                            onClick={() => setShowNewModal(true)}
                            className="px-4 py-2 bg-green-600 hover:bg-green-700 rounded-lg font-medium transition-colors"
                        >
                            Issue Certificate
                        </button>
                    </div>
                ) : (
                    <div className="grid gap-4">
                        {certificates.map((cert) => {
                            const daysUntilExpiry = getDaysUntilExpiry(cert.valid_until)

                            return (
                                <div
                                    key={cert.id}
                                    className="p-6 bg-gray-800 rounded-lg border border-gray-700 hover:border-gray-600 transition-colors"
                                >
                                    <div className="flex items-start justify-between">
                                        <div className="flex-1">
                                            <div className="flex items-center gap-3 mb-2">
                                                {getStatusIcon(cert.status)}
                                                <h3 className="text-lg font-semibold">{cert.domain}</h3>
                                                <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${cert.status === 'active' ? 'bg-green-500/10 text-green-500' :
                                                    cert.status === 'pending' ? 'bg-yellow-500/10 text-yellow-500' :
                                                        'bg-red-500/10 text-red-500'
                                                    }`}>
                                                    {cert.status}
                                                </span>
                                                <span className="px-2 py-0.5 text-xs font-medium rounded-full bg-blue-500/10 text-blue-500">
                                                    {getTypeLabel(cert.type)}
                                                </span>
                                            </div>

                                            <div className="grid grid-cols-2 gap-4 text-sm mt-4">
                                                <div>
                                                    <span className="text-gray-400">Issuer:</span>
                                                    <span className="ml-2 text-gray-200">{cert.issuer || 'N/A'}</span>
                                                </div>
                                                <div>
                                                    <span className="text-gray-400">Subject:</span>
                                                    <span className="ml-2 text-gray-200">{cert.subject || 'N/A'}</span>
                                                </div>
                                                <div>
                                                    <span className="text-gray-400">Valid From:</span>
                                                    <span className="ml-2 text-gray-200">{formatDate(cert.valid_from)}</span>
                                                </div>
                                                <div>
                                                    <span className="text-gray-400">Valid Until:</span>
                                                    <span className={`ml-2 font-medium ${getExpiryColor(daysUntilExpiry)}`}>
                                                        {formatDate(cert.valid_until)}
                                                        {daysUntilExpiry >= 0 && ` (${daysUntilExpiry} days)`}
                                                        {daysUntilExpiry < 0 && ' (Expired)'}
                                                    </span>
                                                </div>
                                                {cert.auto_renew && (
                                                    <div>
                                                        <span className="text-gray-400">Auto-Renew:</span>
                                                        <span className="ml-2 text-green-500">Enabled</span>
                                                    </div>
                                                )}
                                                {cert.last_renewed && (
                                                    <div>
                                                        <span className="text-gray-400">Last Renewed:</span>
                                                        <span className="ml-2 text-gray-200">{formatDate(cert.last_renewed)}</span>
                                                    </div>
                                                )}
                                            </div>
                                        </div>

                                        <div className="flex gap-2 ml-4">
                                            {cert.type === 'letsencrypt' && (
                                                <button
                                                    onClick={() => handleRenew(cert.id)}
                                                    className="p-2 text-blue-400 hover:text-blue-300 hover:bg-blue-500/10 rounded-lg transition-colors"
                                                    title="Renew Certificate"
                                                >
                                                    <RefreshCw className="h-4 w-4" />
                                                </button>
                                            )}
                                            <button
                                                onClick={() => handleDelete(cert.id, cert.domain)}
                                                className="p-2 text-red-400 hover:text-red-300 hover:bg-red-500/10 rounded-lg transition-colors"
                                                title="Delete Certificate"
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            )
                        })}
                    </div>
                )}
            </div>

            {showNewModal && (
                <NewCertificateModal
                    onClose={() => setShowNewModal(false)}
                    onSuccess={() => {
                        setShowNewModal(false)
                        loadCertificates()
                    }}
                />
            )}
        </Layout>
    )
}

interface NewCertificateModalProps {
    onClose: () => void
    onSuccess: () => void
}

function NewCertificateModal({ onClose, onSuccess }: NewCertificateModalProps) {
    const [request, setRequest] = useState<SSLCertificateRequest>({
        site_id: '',
        domain: '',
        type: 'letsencrypt',
        provider: 'letsencrypt',
        auto_renew: true,
        email: ''
    })
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState('')
    const [sites, setSites] = useState<any[]>([])

    useEffect(() => {
        loadSites()
    }, [])

    const loadSites = async () => {
        try {
            const data = await api.getSites()
            setSites(data.sites || [])
        } catch (err) {
            console.error('Failed to load sites:', err)
        }
    }

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault()
        setError('')

        try {
            setLoading(true)
            await api.issueCertificate(request)
            onSuccess()
        } catch (err: any) {
            setError(err.message || 'Failed to issue certificate')
        } finally {
            setLoading(false)
        }
    }

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
            <div className="bg-gray-800 rounded-lg border border-gray-700 max-w-2xl w-full max-h-[90vh] overflow-y-auto">
                <div className="p-6 border-b border-gray-700">
                    <h2 className="text-xl font-bold">Issue SSL Certificate</h2>
                </div>

                <form onSubmit={handleSubmit} className="p-6 space-y-4">
                    {error && (
                        <div className="p-4 bg-red-500/10 border border-red-500/20 rounded-lg text-red-500">
                            {error}
                        </div>
                    )}

                    <div>
                        <label className="block text-sm font-medium mb-2">Site</label>
                        <select
                            value={request.site_id}
                            onChange={(e) => {
                                const site = sites.find(s => s.id === e.target.value)
                                setRequest({ ...request, site_id: e.target.value, domain: site?.domain || '' })
                            }}
                            className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500"
                            required
                        >
                            <option value="">Select a site...</option>
                            {sites.map(site => (
                                <option key={site.id} value={site.id}>{site.name} ({site.domain})</option>
                            ))}
                        </select>
                    </div>

                    <div>
                        <label className="block text-sm font-medium mb-2">Domain</label>
                        <input
                            type="text"
                            value={request.domain}
                            onChange={(e) => setRequest({ ...request, domain: e.target.value })}
                            className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500"
                            placeholder="example.com"
                            required
                        />
                    </div>

                    <div>
                        <label className="block text-sm font-medium mb-2">Certificate Type</label>
                        <select
                            value={request.type}
                            onChange={(e) => setRequest({ ...request, type: e.target.value as any })}
                            className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500"
                            required
                        >
                            <option value="letsencrypt">Let's Encrypt (Free)</option>
                            <option value="self-signed">Self-Signed (Development)</option>
                            <option value="custom">Custom Certificate</option>
                        </select>
                    </div>

                    {request.type === 'letsencrypt' && (
                        <>
                            <div>
                                <label className="block text-sm font-medium mb-2">Provider</label>
                                <select
                                    value={request.provider}
                                    onChange={(e) => setRequest({ ...request, provider: e.target.value as any })}
                                    className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500"
                                >
                                    <option value="letsencrypt">Let's Encrypt</option>
                                    <option value="zerossl">ZeroSSL</option>
                                </select>
                            </div>

                            <div>
                                <label className="block text-sm font-medium mb-2">Email</label>
                                <input
                                    type="email"
                                    value={request.email}
                                    onChange={(e) => setRequest({ ...request, email: e.target.value })}
                                    className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500"
                                    placeholder="admin@example.com"
                                    required
                                />
                                <p className="text-xs text-gray-400 mt-1">
                                    Used for renewal notifications
                                </p>
                            </div>

                            <div className="flex items-center gap-2">
                                <input
                                    type="checkbox"
                                    id="auto-renew"
                                    checked={request.auto_renew}
                                    onChange={(e) => setRequest({ ...request, auto_renew: e.target.checked })}
                                    className="rounded border-gray-700"
                                />
                                <label htmlFor="auto-renew" className="text-sm">
                                    Auto-renew certificate before expiry
                                </label>
                            </div>
                        </>
                    )}

                    {request.type === 'custom' && (
                        <>
                            <div>
                                <label className="block text-sm font-medium mb-2">Certificate (PEM)</label>
                                <textarea
                                    value={request.custom_cert || ''}
                                    onChange={(e) => setRequest({ ...request, custom_cert: e.target.value })}
                                    className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500 font-mono text-sm"
                                    rows={6}
                                    placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                                    required
                                />
                            </div>

                            <div>
                                <label className="block text-sm font-medium mb-2">Private Key (PEM)</label>
                                <textarea
                                    value={request.custom_key || ''}
                                    onChange={(e) => setRequest({ ...request, custom_key: e.target.value })}
                                    className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500 font-mono text-sm"
                                    rows={6}
                                    placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                                    required
                                />
                            </div>

                            <div>
                                <label className="block text-sm font-medium mb-2">CA Chain (Optional)</label>
                                <textarea
                                    value={request.custom_ca || ''}
                                    onChange={(e) => setRequest({ ...request, custom_ca: e.target.value })}
                                    className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg focus:outline-none focus:border-green-500 font-mono text-sm"
                                    rows={4}
                                    placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                                />
                            </div>
                        </>
                    )}

                    <div className="flex gap-3 pt-4">
                        <button
                            type="submit"
                            disabled={loading}
                            className="flex-1 px-4 py-2 bg-green-600 hover:bg-green-700 disabled:bg-gray-700 disabled:cursor-not-allowed rounded-lg font-medium transition-colors"
                        >
                            {loading ? 'Issuing...' : 'Issue Certificate'}
                        </button>
                        <button
                            type="button"
                            onClick={onClose}
                            className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg font-medium transition-colors"
                        >
                            Cancel
                        </button>
                    </div>
                </form>
            </div>
        </div>
    )
}
