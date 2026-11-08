package graph

import (
	"strconv"
	"strings"

	"github.com/vincent/eso-map/internal/eso"
	"github.com/vincent/eso-map/internal/manifest"
)

func workloadID(o manifest.Object) string {
	return "workload:" + o.Kind + "/" + qualified(o.Namespace, o.Name)
}

// addWorkload records every way a pod-based workload reads Secrets: env
// vars, envFrom, secret and projected volumes, and image pull secrets.
// Objects without a pod spec are ignored.
func (b *builder) addWorkload(o manifest.Object) error {
	var w eso.Workload
	if err := o.Decode(&w); err != nil {
		return err
	}
	spec := w.PodSpec()
	if spec == nil {
		return nil
	}

	// Created lazily so workloads that use no Secrets stay out of the graph.
	var n *Node
	wl := func() *Node {
		if n == nil {
			n = b.node(workloadID(o), func() *Node {
				return &Node{Type: NodeWorkload, Kind: o.Kind, Name: o.Name, Namespace: o.Namespace, Label: o.Name, Source: b.source(o)}
			})
		}
		return n
	}
	link := func(secretName, key, itemID string, item Item) {
		node := wl()
		secret := b.consumedSecret(o.Namespace, secretName)
		from := ""
		if key != "" {
			from = b.secretKey(secret, key, o, node.ID)
		}
		item.ID = itemID
		node.AddItem(item)
		b.edge(Edge{From: secret.ID, FromItem: from, To: node.ID, ToItem: itemID, Type: EdgeConsume})
	}

	volumes := map[string]eso.Volume{}
	for _, v := range spec.Volumes {
		volumes[v.Name] = v
	}

	containers := append(append([]eso.Container{}, spec.InitContainers...), spec.Containers...)
	for _, c := range containers {
		for _, e := range c.Env {
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				continue
			}
			ref := e.ValueFrom.SecretKeyRef
			link(ref.Name, ref.Key, "env:"+c.Name+":"+e.Name, Item{
				Label:  "$" + e.Name,
				Detail: "env in container " + c.Name + " ← " + ref.Name + "[" + ref.Key + "]",
			})
		}
		for _, ef := range c.EnvFrom {
			if ef.SecretRef == nil {
				continue
			}
			detail := "all keys as env in container " + c.Name
			if ef.Prefix != "" {
				detail += ", prefix " + ef.Prefix
			}
			link(ef.SecretRef.Name, "", "envFrom:"+c.Name+":"+ef.SecretRef.Name, Item{
				Label:  "envFrom " + ef.SecretRef.Name,
				Detail: detail,
			})
		}
		for _, m := range c.VolumeMounts {
			v, ok := volumes[m.Name]
			if !ok {
				continue
			}
			path := m.MountPath
			if m.SubPath != "" {
				path = strings.TrimSuffix(path, "/") + " (subPath " + m.SubPath + ")"
			}
			itemID := "mount:" + c.Name + ":" + m.MountPath
			item := Item{Label: path, Detail: "volume " + v.Name + " in container " + c.Name}
			for _, src := range secretVolumeSources(v) {
				if len(src.items) == 0 {
					link(src.name, "", itemID, item)
					continue
				}
				for _, kp := range src.items {
					link(src.name, kp.Key, itemID, item)
				}
			}
		}
	}
	for _, ips := range spec.ImagePullSecrets {
		link(ips.Name, "", "imagePullSecret:"+ips.Name, Item{Label: "imagePullSecret " + ips.Name, Detail: "registry credentials"})
	}
	return nil
}

type secretVolumeSource struct {
	name  string
	items []eso.KeyToPath
}

func secretVolumeSources(v eso.Volume) []secretVolumeSource {
	var out []secretVolumeSource
	if v.Secret != nil && v.Secret.SecretName != "" {
		out = append(out, secretVolumeSource{v.Secret.SecretName, v.Secret.Items})
	}
	if v.Projected != nil {
		for _, s := range v.Projected.Sources {
			if s.Secret != nil && s.Secret.Name != "" {
				out = append(out, secretVolumeSource{s.Secret.Name, s.Secret.Items})
			}
		}
	}
	return out
}

// addIngress records TLS certificate Secrets referenced by an Ingress.
func (b *builder) addIngress(o manifest.Object) error {
	var ing eso.Ingress
	if err := o.Decode(&ing); err != nil {
		return err
	}
	for i, tls := range ing.Spec.TLS {
		if tls.SecretName == "" {
			continue
		}
		n := b.node(workloadID(o), func() *Node {
			return &Node{Type: NodeWorkload, Kind: o.Kind, Name: o.Name, Namespace: o.Namespace, Label: o.Name, Source: b.source(o)}
		})
		secret := b.consumedSecret(o.Namespace, tls.SecretName)
		id := "tls:" + strconv.Itoa(i)
		n.AddItem(Item{ID: id, Label: "TLS " + strings.Join(tls.Hosts, ", "), Detail: "certificate from " + tls.SecretName})
		b.edge(Edge{From: secret.ID, To: n.ID, ToItem: id, Type: EdgeConsume})
	}
	return nil
}
