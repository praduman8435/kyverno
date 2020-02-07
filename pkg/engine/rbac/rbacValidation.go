package rbac

import (
	"reflect"
	"sync"

	kyverno "github.com/nirmata/kyverno/pkg/api/kyverno/v1"
	utils "github.com/nirmata/kyverno/pkg/utils"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

const (
	//SaPrefix defines the prefix for service accounts
	SaPrefix = "system:serviceaccount:"
)

// MatchAdmissionInfo return true if the rule can be applied to the request
func MatchAdmissionInfo(rule kyverno.Rule, requestInfo kyverno.RequestInfo) bool {
	// when processing existing resource, it does not contain requestInfo
	// skip permission checking
	if reflect.DeepEqual(requestInfo, kyverno.RequestInfo{}) {
		return true
	}

	if !validateMatch(rule.MatchResources, requestInfo) {
		return false
	}

	return validateExclude(rule.ExcludeResources, requestInfo)
}

// match:
// 		roles: role1, role2
// 		clusterRoles: clusterRole1,clusterRole2
// 		subjects: subject1, subject2
// validateMatch return true if (role1 || role2) and (clusterRole1 || clusterRole2)
// and (subject1 || subject2) are found in requestInfo, OR operation for each list
func validateMatch(match kyverno.MatchResources, requestInfo kyverno.RequestInfo) bool {
	if len(match.Roles) > 0 {
		if !matchRoleRefs(match.Roles, requestInfo.Roles) {
			return false
		}
	}

	if len(match.ClusterRoles) > 0 {
		if !matchRoleRefs(match.ClusterRoles, requestInfo.ClusterRoles) {
			return false
		}
	}

	if len(match.Subjects) > 0 {
		if !matchSubjects(match.Subjects, requestInfo.AdmissionUserInfo) {
			return false
		}
	}
	return true
}

// exclude:
// 		roles: role1, role2
// 		clusterRoles: clusterRole1,clusterRole2
// 		subjects: subject1, subject2
// validateExclude return true if none of the above found in requestInfo
// otherwise return false immediately means rule should not be applied
func validateExclude(exclude kyverno.ExcludeResources, requestInfo kyverno.RequestInfo) bool {
	if reflect.DeepEqual(exclude, kyverno.ExcludeResources{}) {
		return true
	}

	var conditions = make(chan bool, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		if len(exclude.Roles) > 0 {
			conditions <- matchRoleRefs(exclude.Roles, requestInfo.Roles)
		}
		wg.Done()
	}()

	go func() {
		if len(exclude.ClusterRoles) > 0 {
			conditions <- matchRoleRefs(exclude.ClusterRoles, requestInfo.ClusterRoles)
		}
		wg.Done()
	}()

	go func() {
		if len(exclude.Subjects) > 0 {
			conditions <- matchSubjects(exclude.Subjects, requestInfo.AdmissionUserInfo)
		}
		wg.Done()
	}()

	wg.Wait()
	close(conditions)

	var isValid bool
	for hasPassed := range conditions {
		if !hasPassed {
			isValid = true
			break
		}
	}

	return isValid
}

// matchRoleRefs return true if one of ruleRoleRefs exist in resourceRoleRefs
func matchRoleRefs(ruleRoleRefs, resourceRoleRefs []string) bool {
	for _, ruleRoleRef := range ruleRoleRefs {
		if utils.ContainsString(resourceRoleRefs, ruleRoleRef) {
			return true
		}
	}
	return false
}

// matchSubjects return true if one of ruleSubjects exist in userInfo
func matchSubjects(ruleSubjects []rbacv1.Subject, userInfo authenticationv1.UserInfo) bool {
	userGroups := append(userInfo.Groups, userInfo.Username)
	for _, subject := range ruleSubjects {
		switch subject.Kind {
		case "ServiceAccount":
			if len(userInfo.Username) <= len(SaPrefix) {
				continue
			}
			subjectServiceAccount := subject.Namespace + ":" + subject.Name
			if userInfo.Username[len(SaPrefix):] == subjectServiceAccount {
				return true
			}
		case "User", "Group":
			if utils.ContainsString(userGroups, subject.Name) {
				return true
			}
		}
	}

	return false
}
